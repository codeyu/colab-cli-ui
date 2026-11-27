package main

import (
	"archive/zip"
	"context"
	"crypto/rand"
	_ "embed"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/egoist/mygo"
	"golang.org/x/crypto/ssh"
)

// The task library: work done on Colab, kept to run again with one click.
// A batch task is a Python script that turns the files of $TASK_INPUT into
// files in $TASK_OUTPUT. Running it takes a session (or creates one), copies
// the local inputs to the VM, runs the script as a detached job, brings the
// output back into a local folder and releases the VM. A service task starts
// something to reach through a forwarded port, such as a model's API.

// TaskOption is a choice of a select parameter.
type TaskOption struct {
	Value string `json:"value"`
	Label string `json:"label"`
	Hint  string `json:"hint,omitempty"`
}

// TaskParam is a field of a task's form; the script gets the values in the
// JSON file $TASK_PARAMS.
type TaskParam struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	// Type is "select", "text", "number", "bool" or "multi" (several of
	// Options, comma-separated).
	Type    string       `json:"type"`
	Default string       `json:"default,omitempty"`
	Help    string       `json:"help,omitempty"`
	Options []TaskOption `json:"options,omitempty"`
	// ShowIf shows the field only when another has a value: "model=custom".
	ShowIf string `json:"showIf,omitempty"`
}

// TaskDef is a task of the library.
type TaskDef struct {
	ID string `json:"id"`
	// Kind is "batch" (files in, files out), "service" (something to reach
	// through a port) or "setup" (code run in the kernel).
	Kind        string `json:"kind"`
	Name        string `json:"name"`
	Category    string `json:"category"`
	Description string `json:"description"`
	Builtin     bool   `json:"builtin"`
	// Hardware is the suggested accelerator (as in NewSessionOptions), and
	// HardwareNote why.
	Hardware     string      `json:"hardware"`
	HardwareNote string      `json:"hardwareNote,omitempty"`
	Params       []TaskParam `json:"params"`
	// InputLabel and OutputLabel describe a batch task's files.
	InputLabel  string `json:"inputLabel,omitempty"`
	OutputLabel string `json:"outputLabel,omitempty"`
	// Script is a batch task's Python script.
	Script string `json:"script,omitempty"`
	// Steps are a setup task's code, run one after another in the kernel.
	Steps []string `json:"steps,omitempty"`
	// Drive mounts Google Drive before a setup task's steps.
	Drive bool `json:"drive,omitempty"`
	// Service names what a service task opens ("llm-api").
	Service string `json:"service,omitempty"`
	Updated int64  `json:"updated,omitempty"`
}

//go:embed tasks/asr.py
var asrScript string

var languages = []TaskOption{
	{Value: "auto", Label: "自动识别"},
	{Value: "zh", Label: "中文"},
	{Value: "en", Label: "英语"},
	{Value: "ja", Label: "日语"},
	{Value: "ko", Label: "韩语"},
	{Value: "yue", Label: "粤语"},
	{Value: "fr", Label: "法语"},
	{Value: "de", Label: "德语"},
	{Value: "es", Label: "西班牙语"},
	{Value: "ru", Label: "俄语"},
}

func builtinTasks() []TaskDef {
	return []TaskDef{
		{
			ID:       "asr-batch",
			Kind:     "batch",
			Name:     "批量语音识别",
			Category: "语音",
			Description: "把一批音频或视频转成文字和字幕（txt / srt / vtt / json）。可选 Whisper 系列（faster-whisper）" +
				"或 SenseVoice 等开源模型，GPU 上通常几十倍于实时。",
			Builtin:      true,
			Hardware:     "T4",
			HardwareNote: "T4 足够运行 Whisper large-v3；small 和 SenseVoice 在 CPU 上也能跑，但慢得多",
			Params: []TaskParam{
				{ID: "model", Label: "模型", Type: "select", Default: "large-v3-turbo", Options: []TaskOption{
					{Value: "large-v3-turbo", Label: "Whisper large-v3-turbo", Hint: "推荐：多语言，准确且快"},
					{Value: "large-v3", Label: "Whisper large-v3", Hint: "多语言，最准，较慢"},
					{Value: "distil-large-v3", Label: "Distil-Whisper large-v3", Hint: "只适合英语，很快"},
					{Value: "medium", Label: "Whisper medium", Hint: "多语言，中等"},
					{Value: "small", Label: "Whisper small", Hint: "多语言，CPU 也能跑"},
					{Value: "kotoba-tech/kotoba-whisper-v2.0-faster", Label: "Kotoba-Whisper v2.0", Hint: "日语专用，快"},
					{Value: "FunAudioLLM/SenseVoiceSmall", Label: "SenseVoice Small", Hint: "中、粤、英、日、韩，极快"},
					{Value: "custom", Label: "自定义", Hint: "任意 faster-whisper（CTranslate2）格式的 HuggingFace 仓库"},
				}},
				{ID: "custom_model", Label: "HuggingFace 仓库", Type: "text", ShowIf: "model=custom",
					Help: "例如 deepdml/faster-whisper-large-v3-turbo-ct2"},
				{ID: "language", Label: "语言", Type: "select", Default: "auto", Options: languages,
					Help: "知道语言时指定，更准；SenseVoice 只支持中、粤、英、日、韩"},
				{ID: "formats", Label: "输出格式", Type: "multi", Default: "txt,srt", Options: []TaskOption{
					{Value: "txt", Label: "txt"}, {Value: "srt", Label: "srt"}, {Value: "vtt", Label: "vtt"}, {Value: "json", Label: "json"},
				}},
			},
			InputLabel:  "音频或视频文件（mp3、wav、m4a、flac、mp4、mkv 等），可选文件夹",
			OutputLabel: "转写结果保存到",
			Script:      asrScript,
		},
		{
			ID:           "llm-api",
			Kind:         "service",
			Name:         "大模型 API",
			Category:     "大模型",
			Description:  "在 VM 上用 Ollama 运行开源大模型，转发成本机的 OpenAI 兼容 API，可接入任何支持自定义 Base URL 的客户端。",
			Builtin:      true,
			Hardware:     "T4",
			HardwareNote: "8B 模型用 T4 即可，27B 以上建议 L4 或 A100",
			Params:       []TaskParam{},
			Service:      "llm-api",
		},
	}
}

var taskID = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,47}$`)

func tasksPath() (string, error) {
	dir, err := mygo.App.Path(mygo.PathUserData)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "tasks.json"), nil
}

var tasksMu sync.Mutex

func loadUserTasks() ([]TaskDef, error) {
	p, err := tasksPath()
	if err != nil {
		return nil, err
	}
	b, err := os.ReadFile(p)
	if errors.Is(err, os.ErrNotExist) {
		return []TaskDef{}, nil
	}
	if err != nil {
		return nil, err
	}
	var list []TaskDef
	if err := json.Unmarshal(b, &list); err != nil {
		return nil, fmt.Errorf("任务库文件损坏：%w", err)
	}
	return list, nil
}

func saveUserTasks(list []TaskDef) error {
	p, err := tasksPath()
	if err != nil {
		return err
	}
	b, _ := json.MarshalIndent(list, "", "  ")
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}

// ListTasks lists the built-in tasks, then the user's.
func (c *Colab) ListTasks() ([]TaskDef, error) {
	tasksMu.Lock()
	defer tasksMu.Unlock()
	user, err := loadUserTasks()
	if err != nil {
		return nil, err
	}
	return append(builtinTasks(), user...), nil
}

func (c *Colab) findTask(id string) (TaskDef, error) {
	list, err := c.ListTasks()
	if err != nil {
		return TaskDef{}, err
	}
	for _, t := range list {
		if t.ID == id {
			return t, nil
		}
	}
	return TaskDef{}, fmt.Errorf("任务库里没有 %s", id)
}

func checkTask(t *TaskDef) error {
	t.Name = strings.TrimSpace(t.Name)
	if t.Name == "" {
		return errors.New("任务需要一个名字")
	}
	switch t.Kind {
	case "batch":
		if strings.TrimSpace(t.Script) == "" {
			return errors.New("批处理任务需要脚本")
		}
	case "setup":
		if len(t.Steps) == 0 {
			return errors.New("准备型任务至少要有一段代码")
		}
	default:
		return fmt.Errorf("不支持的任务类型：%q", t.Kind)
	}
	if t.Hardware == "" {
		t.Hardware = "CPU"
	}
	if t.Params == nil {
		t.Params = []TaskParam{}
	}
	for _, p := range t.Params {
		if !regexp.MustCompile(`^[a-z][a-z0-9_]{0,31}$`).MatchString(p.ID) {
			return fmt.Errorf("参数名无效：%q", p.ID)
		}
	}
	return nil
}

// SaveTask adds a task to the library, or replaces the user task with its
// ID. A new task gets an ID of its own.
func (c *Colab) SaveTask(t TaskDef) (TaskDef, error) {
	if err := checkTask(&t); err != nil {
		return TaskDef{}, err
	}
	t.Builtin = false
	t.Service = ""
	t.Updated = time.Now().UnixMilli()
	tasksMu.Lock()
	defer tasksMu.Unlock()
	list, err := loadUserTasks()
	if err != nil {
		return TaskDef{}, err
	}
	for _, b := range builtinTasks() {
		if b.ID == t.ID {
			t.ID = ""
		}
	}
	if !taskID.MatchString(t.ID) {
		b := make([]byte, 4)
		rand.Read(b)
		t.ID = "task-" + hex.EncodeToString(b)
	}
	replaced := false
	for i := range list {
		if list[i].ID == t.ID {
			list[i], replaced = t, true
		}
	}
	if !replaced {
		list = append(list, t)
	}
	return t, saveUserTasks(list)
}

// DeleteTask removes a user task.
func (c *Colab) DeleteTask(id string) error {
	tasksMu.Lock()
	defer tasksMu.Unlock()
	list, err := loadUserTasks()
	if err != nil {
		return err
	}
	out := list[:0]
	for _, t := range list {
		if t.ID != id {
			out = append(out, t)
		}
	}
	return saveUserTasks(out)
}

// ExportTask saves a task as a .colabtask.json file where the user picks.
func (c *Colab) ExportTask(ctx context.Context, id string) (string, error) {
	t, err := c.findTask(id)
	if err != nil {
		return "", err
	}
	if t.Kind == "service" {
		return "", errors.New("内置的服务型任务不能导出")
	}
	local, err := mygo.Dialog.Save(mygo.SaveDialogOptions{
		Parent:      mygo.CallerWindow(ctx),
		Title:       "导出任务",
		DefaultPath: t.Name + ".colabtask.json",
		Filters:     []mygo.FileFilter{{Name: "Colab 任务", Extensions: []string{"json"}}},
	})
	if err != nil || local == "" {
		return "", err
	}
	t.Builtin, t.Updated = false, 0
	b, _ := json.MarshalIndent(t, "", "  ")
	return local, os.WriteFile(local, b, 0o644)
}

// ImportTask reads a task file the user picks, for the page to show before
// it is saved; it does not add it to the library.
func (c *Colab) ImportTask(ctx context.Context) (*TaskDef, error) {
	files, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
		Parent:  mygo.CallerWindow(ctx),
		Title:   "导入任务",
		Filters: []mygo.FileFilter{{Name: "Colab 任务", Extensions: []string{"json"}}},
	})
	if err != nil || len(files) == 0 {
		return nil, err
	}
	b, err := os.ReadFile(files[0])
	if err != nil {
		return nil, err
	}
	if len(b) > 1<<20 {
		return nil, errors.New("任务文件太大")
	}
	var t TaskDef
	if err := json.Unmarshal(b, &t); err != nil {
		return nil, fmt.Errorf("不是任务文件：%w", err)
	}
	t.ID = "" // a new entry, never a built-in or existing one
	if err := checkTask(&t); err != nil {
		return nil, err
	}
	return &t, nil
}

// PickTaskInputs asks for the files or a folder to process.
func (c *Colab) PickTaskInputs(ctx context.Context, folder bool) ([]string, error) {
	opts := mygo.OpenDialogOptions{Parent: mygo.CallerWindow(ctx), Title: "选择要处理的文件", Multiple: true}
	if folder {
		opts.Title = "选择要处理的文件夹"
		opts.Directory = true
		opts.Multiple = false
	}
	files, err := mygo.Dialog.Open(opts)
	if files == nil {
		files = []string{}
	}
	return files, err
}

// PickFolder asks for a folder, as where to save a task's output.
func (c *Colab) PickFolder(ctx context.Context, title string) (string, error) {
	files, err := mygo.Dialog.Open(mygo.OpenDialogOptions{Parent: mygo.CallerWindow(ctx), Title: title, Directory: true})
	if err != nil || len(files) == 0 {
		return "", err
	}
	return files[0], nil
}

// ---------------------------------------------------------------------------
// Running a batch task

// TaskRunRequest says how to run a batch task.
type TaskRunRequest struct {
	TaskID string `json:"taskId"`
	// Session runs it there; with Create it is the name of the session to
	// create, with Create's accelerator.
	Session string             `json:"session"`
	Create  *NewSessionOptions `json:"create,omitempty"`
	Params  map[string]string  `json:"params"`
	// Inputs are local files and folders, OutputDir where results go.
	Inputs    []string `json:"inputs"`
	OutputDir string   `json:"outputDir"`
	// Release stops the session when the task ends.
	Release bool `json:"release"`
}

const taskRoot = "/content/colab-ui-tasks"

// taskWrapper runs a task script as a job: %s are Python literals of the job
// directory and the run directory.
const taskWrapper = `import json, os, shutil, subprocess, sys
job, run = %s, %s
def status(**kw):
    tmp = os.path.join(job, "status.json.tmp")
    with open(tmp, "w") as fh:
        json.dump(kw, fh)
    os.replace(tmp, os.path.join(job, "status.json"))
status(state="running")
os.makedirs(os.path.join(run, "out"), exist_ok=True)
env = dict(os.environ, TASK_PARAMS=os.path.join(run, "params.json"), TASK_INPUT=os.path.join(run, "in"),
           TASK_OUTPUT=os.path.join(run, "out"), TASK_RUN=run)
rc = subprocess.call([sys.executable, "-u", os.path.join(run, "task.py")], env=env, cwd=run, stderr=subprocess.STDOUT)
if rc:
    status(state="error", error="任务脚本出错（退出码 %%d），详见上面的输出" %% rc)
else:
    n = sum(len(fs) for _, _, fs in os.walk(os.path.join(run, "out")))
    shutil.make_archive(os.path.join(run, "out"), "zip", os.path.join(run, "out"))
    print("打包结果：%%d 个文件，%%.1f MB" %% (n, os.path.getsize(os.path.join(run, "out.zip")) / 1e6), flush=True)
    status(state="done")
`

// RunTask runs a batch task, streaming each step.
func (c *Colab) RunTask(ctx context.Context, req TaskRunRequest, out *mygo.Channel[Chunk]) (err error) {
	t, err := c.findTask(req.TaskID)
	if err != nil {
		return err
	}
	if t.Kind != "batch" {
		return errors.New("这个任务不是批处理任务")
	}
	if err := checkName(req.Session); err != nil {
		return err
	}
	if len(req.Inputs) == 0 {
		return errors.New("没有选择要处理的文件")
	}
	if req.OutputDir == "" {
		return errors.New("没有选择结果保存位置")
	}
	info := func(format string, a ...any) {
		_ = out.Send(Chunk{Stream: "info", Text: fmt.Sprintf(format, a...) + "\n"})
	}
	name := req.Session
	// Check the inputs before a VM is billed for them.
	files, err := collectInputs(req.Inputs)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(req.OutputDir, 0o755); err != nil {
		return fmt.Errorf("结果保存位置不可用：%w", err)
	}
	// Pin the account, so that a switch while the task runs changes nothing.
	ctx = withAccount(ctx, c.accountFor(ctx, nil))

	if req.Create != nil {
		req.Create.Name = name
		info("== 新建 %s 会话 %s", req.Create.Accelerator, name)
		if err := c.CreateSession(ctx, *req.Create, out); err != nil {
			return err
		}
		s := c.settings()
		if s.GuardHours > 0 || s.GuardIdleMinutes > 0 {
			if _, err := c.SetGuard(ctx, name, s.GuardHours, s.GuardIdleMinutes); err != nil {
				info("自动释放没有启动：%v", err)
			}
		}
	}
	if req.Release {
		defer func() {
			// Also after a failure or a cancel: the VM would bill on idle.
			sctx, cancel := context.WithTimeout(withAccount(context.Background(), c.accountFor(ctx, nil)), 2*time.Minute)
			defer cancel()
			if _, serr := c.StopSession(sctx, name); serr != nil {
				info("释放会话失败，请手动释放：%v", serr)
			} else {
				info("== 已释放会话 %s", name)
			}
		}()
	}

	idb := make([]byte, 6)
	rand.Read(idb)
	run := taskRoot + "/" + time.Now().Format("20060102-150405") + "-" + hex.EncodeToString(idb[:3])

	link, release, err := c.holdLink(ctx, name)
	if err != nil {
		return err
	}
	defer release()
	cl, err := link.get(ctx, c)
	if err != nil {
		return fmt.Errorf("连接 VM 失败：%w", err)
	}

	info("== 上传输入文件")
	var total int64
	for _, f := range files {
		total += f.size
	}
	info("%d 个文件，共 %.1f MB", len(files), float64(total)/1e6)
	start := time.Now()
	for i, f := range files {
		if err := ctx.Err(); err != nil {
			return errors.New("已取消")
		}
		_ = out.Send(Chunk{Stream: "stdout", Text: fmt.Sprintf("[%d/%d] %s\n", i+1, len(files), f.rel)})
		if err := sshPut(cl, f.local, run+"/in/"+f.rel); err != nil {
			return fmt.Errorf("上传 %s 失败：%w", f.rel, err)
		}
	}
	info("上传完成，用时 %.0fs", time.Since(start).Seconds())

	params, _ := json.Marshal(req.Params)
	if err := sshWrite(cl, run+"/params.json", params); err != nil {
		return err
	}
	if err := sshWrite(cl, run+"/task.py", []byte(t.Script)); err != nil {
		return err
	}

	info("== 运行 %s", t.Name)
	job := newJobDir()
	if err := c.runJob(ctx, name, job, fmt.Sprintf(taskWrapper, pyLiteral(job), pyLiteral(run)), "任务", out); err != nil {
		return err
	}

	info("== 下载结果")
	// Again: the connection may have dropped during a long job.
	if cl, err = link.get(ctx, c); err != nil {
		return fmt.Errorf("连接 VM 失败：%w", err)
	}
	tmp, err := os.CreateTemp("", "colab-task-*.zip")
	if err != nil {
		return err
	}
	tmp.Close()
	defer os.Remove(tmp.Name())
	if err := sshGet(cl, run+"/out.zip", tmp.Name()); err != nil {
		return fmt.Errorf("下载结果失败：%w", err)
	}
	n, err := unzipInto(tmp.Name(), req.OutputDir)
	if err != nil {
		return err
	}
	info("== 完成：%d 个文件保存到 %s", n, req.OutputDir)
	return nil
}

type inputFile struct {
	local, rel string
	size       int64
}

// collectInputs lists the files to upload: a file as its name, a folder's
// files under the folder's name.
func collectInputs(paths []string) ([]inputFile, error) {
	var list []inputFile
	seen := map[string]bool{}
	add := func(local, rel string, size int64) {
		rel = filepath.ToSlash(rel)
		if !seen[rel] {
			seen[rel] = true
			list = append(list, inputFile{local, rel, size})
		}
	}
	for _, p := range paths {
		st, err := os.Stat(p)
		if err != nil {
			return nil, err
		}
		if !st.IsDir() {
			add(p, filepath.Base(p), st.Size())
			continue
		}
		base := filepath.Base(p)
		err = filepath.WalkDir(p, func(f string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || strings.HasPrefix(d.Name(), ".") {
				return err
			}
			fi, err := d.Info()
			if err != nil {
				return err
			}
			rel, _ := filepath.Rel(p, f)
			add(f, filepath.Join(base, rel), fi.Size())
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	if len(list) == 0 {
		return nil, errors.New("选中的位置里没有文件")
	}
	return list, nil
}

func shQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

// sshPut copies a local file to a path on the VM, making its directory.
func sshPut(cl *ssh.Client, local, remote string) error {
	f, err := os.Open(local)
	if err != nil {
		return err
	}
	defer f.Close()
	s, err := cl.NewSession()
	if err != nil {
		return err
	}
	defer s.Close()
	s.Stdin = f
	out, err := s.CombinedOutput("mkdir -p " + shQuote(path.Dir(remote)) + " && cat > " + shQuote(remote))
	if err != nil {
		return fmt.Errorf("%v %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func sshWrite(cl *ssh.Client, remote string, data []byte) error {
	s, err := cl.NewSession()
	if err != nil {
		return err
	}
	defer s.Close()
	// base64, so that no byte of it reaches a shell.
	s.Stdin = strings.NewReader(base64.StdEncoding.EncodeToString(data))
	out, err := s.CombinedOutput("mkdir -p " + shQuote(path.Dir(remote)) + " && base64 -d > " + shQuote(remote))
	if err != nil {
		return fmt.Errorf("写入 %s 失败：%v %s", remote, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// sshGet copies a file on the VM to a local path.
func sshGet(cl *ssh.Client, remote, local string) error {
	f, err := os.Create(local)
	if err != nil {
		return err
	}
	defer f.Close()
	s, err := cl.NewSession()
	if err != nil {
		return err
	}
	defer s.Close()
	s.Stdout = f
	var stderr strings.Builder
	s.Stderr = &stderr
	if err := s.Run("cat " + shQuote(remote)); err != nil {
		return fmt.Errorf("%v %s", err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

// unzipInto extracts a zip into dir, refusing paths that leave it, and
// returns how many files it wrote.
func unzipInto(zipPath, dir string) (int, error) {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return 0, err
	}
	defer r.Close()
	root, err := filepath.Abs(dir)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, f := range r.File {
		target := filepath.Join(root, filepath.FromSlash(f.Name))
		if target != root && !strings.HasPrefix(target, root+string(filepath.Separator)) {
			return n, fmt.Errorf("结果里有越出目录的路径：%s", f.Name)
		}
		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return n, err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return n, err
		}
		src, err := f.Open()
		if err != nil {
			return n, err
		}
		dst, err := os.Create(target)
		if err != nil {
			src.Close()
			return n, err
		}
		_, err = io.Copy(dst, src)
		src.Close()
		dst.Close()
		if err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

// OpenFolder opens a local folder in the file manager, as a task's output.
func (c *Colab) OpenFolder(dir string) error {
	st, err := os.Stat(dir)
	if err != nil {
		return err
	}
	if !st.IsDir() {
		return errors.New("不是文件夹")
	}
	return mygo.Shell.OpenPath(dir)
}
