# Colab CLI UI task: batch speech recognition.
#
# Transcribes every audio or video file under $TASK_INPUT into $TASK_OUTPUT
# (same relative paths), as .txt / .srt / .vtt / .json. Parameters come from
# the JSON file $TASK_PARAMS: model, custom_model, language, formats.
#
# Whisper models run with faster-whisper (CTranslate2): float16 on a GPU,
# int8 on a CPU. SenseVoice runs with FunASR, split into speech segments by
# its VAD model so that subtitles get timestamps.
import glob
import json
import logging
import os
import shutil
import subprocess
import sys
import time

# Keep the log to progress: model downloads would print every HTTP request.
os.environ.setdefault("HF_HUB_VERBOSITY", "error")
os.environ.setdefault("HF_HUB_DISABLE_PROGRESS_BARS", "1")


def quiet():
    for name in ("httpx", "httpcore", "huggingface_hub", "urllib3", "modelscope", "funasr", "root"):
        logging.getLogger(None if name == "root" else name).setLevel(logging.WARNING)


quiet()
P = json.load(open(os.environ["TASK_PARAMS"], encoding="utf-8"))
IN, OUT = os.environ["TASK_INPUT"], os.environ["TASK_OUTPUT"]
MODEL = (P.get("model") or "large-v3-turbo").strip()
if MODEL == "custom":
    MODEL = (P.get("custom_model") or "").strip()
    if not MODEL:
        sys.exit("请填写自定义模型的 HuggingFace 仓库名")
LANG = P.get("language") or "auto"
FORMATS = [f for f in (P.get("formats") or "txt,srt").split(",") if f]
FUNASR = {"FunAudioLLM/SenseVoiceSmall"}
MEDIA = {".mp3", ".wav", ".m4a", ".flac", ".ogg", ".opus", ".aac", ".wma", ".amr",
         ".mp4", ".mkv", ".mov", ".webm", ".avi", ".flv", ".m4v"}


def has_gpu():
    try:
        return subprocess.run(["nvidia-smi"], capture_output=True).returncode == 0
    except OSError:
        return False


GPU = has_gpu()

# CTranslate2 needs cuDNN 9 and cuBLAS 12, which Colab has as pip packages
# (for PyTorch) but not on the library path: add them and start again.
if GPU and MODEL not in FUNASR and not os.environ.get("_TASK_LIBS"):
    dirs = []
    for mod in ("nvidia.cudnn", "nvidia.cublas"):
        try:
            m = __import__(mod, fromlist=["_"])
            dirs += [os.path.join(p, "lib") for p in m.__path__]
        except ImportError:
            pass
    os.environ["LD_LIBRARY_PATH"] = ":".join(dirs + [os.environ.get("LD_LIBRARY_PATH", "")])
    os.environ["_TASK_LIBS"] = "1"
    os.execv(sys.executable, [sys.executable, "-u"] + sys.argv)

files = sorted(p for p in glob.glob(os.path.join(IN, "**", "*"), recursive=True)
               if os.path.isfile(p) and os.path.splitext(p)[1].lower() in MEDIA)
if not files:
    sys.exit("没有找到音频或视频文件（支持 %s）" % " ".join(sorted(MEDIA)))


def pip(*pkgs):
    print("安装 %s…" % " ".join(pkgs), flush=True)
    subprocess.run([sys.executable, "-m", "pip", "install", "-q", *pkgs], check=True)


def stamp(sec, comma=True):
    ms = int(round(sec * 1000))
    h, ms = divmod(ms, 3600000)
    m, ms = divmod(ms, 60000)
    s, ms = divmod(ms, 1000)
    return "%02d:%02d:%02d%s%03d" % (h, m, s, "," if comma else ".", ms)


def write(src, segs, lang, duration):
    """segs: [(start, end, text)] in seconds."""
    base = os.path.join(OUT, os.path.splitext(os.path.relpath(src, IN))[0])
    os.makedirs(os.path.dirname(base), exist_ok=True)
    segs = [(a, b, t.strip()) for a, b, t in segs if t.strip()]
    if "txt" in FORMATS:
        with open(base + ".txt", "w", encoding="utf-8") as fh:
            fh.write("\n".join(t for _, _, t in segs) + "\n")
    if "srt" in FORMATS:
        with open(base + ".srt", "w", encoding="utf-8") as fh:
            for i, (a, b, t) in enumerate(segs, 1):
                fh.write("%d\n%s --> %s\n%s\n\n" % (i, stamp(a), stamp(b), t))
    if "vtt" in FORMATS:
        with open(base + ".vtt", "w", encoding="utf-8") as fh:
            fh.write("WEBVTT\n\n")
            for a, b, t in segs:
                fh.write("%s --> %s\n%s\n\n" % (stamp(a, False), stamp(b, False), t))
    if "json" in FORMATS:
        with open(base + ".json", "w", encoding="utf-8") as fh:
            json.dump({"file": os.path.relpath(src, IN), "language": lang, "duration": duration, "model": MODEL,
                       "segments": [{"start": round(a, 3), "end": round(b, 3), "text": t} for a, b, t in segs]},
                      fh, ensure_ascii=False, indent=1)


def load(path):
    """The file's audio as 16 kHz mono float32, decoded by ffmpeg (video too)."""
    import numpy as np
    raw = subprocess.run(["ffmpeg", "-nostdin", "-v", "error", "-i", path, "-vn", "-f", "f32le", "-ac", "1", "-ar", "16000", "-"],
                         capture_output=True, check=True).stdout
    return np.frombuffer(raw, dtype=np.float32)


def done(dur, t0):
    took = time.time() - t0
    print("\r  ✓ 音频 %.0fs，用时 %.1fs（%.1fx 实时）%s" % (dur, took, dur / max(took, 1e-6), " " * 10), flush=True)


print("模型 %s · 语言 %s · %s · %d 个文件" % (MODEL, LANG, "GPU" if GPU else "CPU", len(files)), flush=True)
t_all, total_audio = time.time(), 0.0

if MODEL in FUNASR:
    try:
        import funasr  # noqa: F401
    except ImportError:
        pip("funasr")
    from funasr import AutoModel
    from funasr.utils.postprocess_utils import rich_transcription_postprocess
    quiet()  # FunASR sets up logging again as it loads
    dev = "cuda:0" if GPU else "cpu"
    print("加载模型…", flush=True)
    vad = AutoModel(model="fsmn-vad", hub="hf", device=dev, disable_update=True, disable_pbar=True, log_level="ERROR")
    asr = AutoModel(model=MODEL, hub="hf", device=dev, disable_update=True, disable_pbar=True, log_level="ERROR")

    for i, f in enumerate(files, 1):
        print("[%d/%d] %s" % (i, len(files), os.path.relpath(f, IN)), flush=True)
        t0 = time.time()
        audio = load(f)
        dur = len(audio) / 16000
        spans = vad.generate(input=audio, fs=16000, disable_pbar=True)[0]["value"] if len(audio) else []
        segs = []
        step = 32
        for j in range(0, len(spans), step):
            part = spans[j:j + step]
            res = asr.generate(input=[audio[a * 16:b * 16] for a, b in part], language=LANG, use_itn=True,
                               batch_size=len(part), disable_pbar=True)
            for (a, b), r in zip(part, res):
                segs.append((a / 1000, b / 1000, rich_transcription_postprocess(r["text"])))
            print("\r  转写中 %3.0f%%" % (100 * min(j + step, len(spans)) / max(len(spans), 1)), end="", flush=True)
        write(f, segs, LANG, dur)
        total_audio += dur
        done(dur, t0)
else:
    try:
        import faster_whisper  # noqa: F401
    except ImportError:
        pip("faster-whisper")
    from faster_whisper import WhisperModel
    print("加载模型…", flush=True)
    model = WhisperModel(MODEL, device="cuda" if GPU else "cpu", compute_type="float16" if GPU else "int8")
    for i, f in enumerate(files, 1):
        print("[%d/%d] %s" % (i, len(files), os.path.relpath(f, IN)), flush=True)
        t0 = time.time()
        # Decoded here: faster-whisper's own decoder breaks with newer PyAV.
        segments, info = model.transcribe(load(f), language=None if LANG == "auto" else LANG, vad_filter=True, beam_size=5)
        segs, last = [], 0.0
        for s in segments:
            segs.append((s.start, s.end, s.text))
            if time.time() - last > 2:
                print("\r  转写中 %3.0f%%" % min(100, 100 * s.end / max(info.duration, 1e-6)), end="", flush=True)
                last = time.time()
        write(f, segs, info.language, info.duration)
        total_audio += info.duration
        done(info.duration, t0)

took = time.time() - t_all
print("全部完成：%d 个文件，音频共 %.1f 分钟，用时 %.0fs" % (len(files), total_audio / 60, took), flush=True)
