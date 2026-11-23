package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/egoist/mygo"
)

// HuggingFace Hub: search models, read a model's files, and download them
// onto a session's VM.

const hfSecretKey = "huggingface-token"

var (
	hfRepoID   = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]*(/[A-Za-z0-9][A-Za-z0-9_.-]*)?$`)
	hfFileName = regexp.MustCompile(`^[^\x00-\x1f]+$`)
	hfHTTP     = &http.Client{Timeout: 30 * time.Second}
)

// HFModel is a search result.
type HFModel struct {
	ID           string `json:"id"`
	Downloads    int64  `json:"downloads"`
	Likes        int64  `json:"likes"`
	PipelineTag  string `json:"pipelineTag"`
	LibraryName  string `json:"libraryName"`
	LastModified string `json:"lastModified"`
	Gated        bool   `json:"gated"`
}

// HFFile is one file of a model repository.
type HFFile struct {
	Name string `json:"name"`
	Size int64  `json:"size"`
}

// HFModelDetail is a model with its files.
type HFModelDetail struct {
	HFModel
	License    string           `json:"license"`
	Parameters int64            `json:"parameters"` // from safetensors metadata; 0 when unknown
	DTypes     map[string]int64 `json:"dtypes"`
	Files      []HFFile         `json:"files"`
	Tags       []string         `json:"tags"`
}

type hfRawModel struct {
	ID           string          `json:"id"`
	Downloads    int64           `json:"downloads"`
	Likes        int64           `json:"likes"`
	PipelineTag  string          `json:"pipeline_tag"`
	LibraryName  string          `json:"library_name"`
	LastModified string          `json:"lastModified"`
	Gated        json.RawMessage `json:"gated"` // false, "auto" or "manual"
	Tags         []string        `json:"tags"`
	CardData     struct {
		License any `json:"license"`
	} `json:"cardData"`
	Safetensors *struct {
		Parameters map[string]int64 `json:"parameters"`
		Total      int64            `json:"total"`
	} `json:"safetensors"`
	Siblings []struct {
		Name string `json:"rfilename"`
		Size int64  `json:"size"`
	} `json:"siblings"`
}

func (r hfRawModel) summary() HFModel {
	g := strings.TrimSpace(string(r.Gated))
	return HFModel{
		ID: r.ID, Downloads: r.Downloads, Likes: r.Likes, PipelineTag: r.PipelineTag,
		LibraryName: r.LibraryName, LastModified: r.LastModified,
		Gated: g != "" && g != "false" && g != "null",
	}
}

func (c *Colab) hfEndpoint() string {
	e := strings.TrimRight(c.settings().HFEndpoint, "/")
	if e == "" {
		e = "https://huggingface.co"
	}
	return e
}

func (c *Colab) hfGet(ctx context.Context, p string, q url.Values, v any) error {
	u := c.hfEndpoint() + p
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "colab-cli-ui")
	// The token only goes to huggingface.co itself, never to a mirror.
	if parsed, _ := url.Parse(u); parsed != nil && parsed.Host == "huggingface.co" {
		if tok, _ := mygo.Secrets.Get(hfSecretKey); len(tok) > 0 {
			req.Header.Set("Authorization", "Bearer "+string(tok))
		}
	}
	resp, err := hfHTTP.Do(req)
	if err != nil {
		return fmt.Errorf("无法连接 HuggingFace：%w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return errors.New("找不到这个模型")
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HuggingFace 返回 %s", resp.Status)
	}
	return json.NewDecoder(resp.Body).Decode(v)
}

// HFSearch searches the Hub's models. task filters by pipeline tag ("" for
// all, or "gguf" for GGUF files); sort is downloads, likes, trendingScore or
// lastModified.
func (c *Colab) HFSearch(ctx context.Context, query, task, sort string) ([]HFModel, error) {
	q := url.Values{"limit": {"50"}, "direction": {"-1"}}
	switch sort {
	case "likes", "trendingScore", "lastModified":
		q.Set("sort", sort)
	default:
		q.Set("sort", "downloads")
	}
	if query = strings.TrimSpace(query); query != "" {
		q.Set("search", query)
	}
	switch task {
	case "":
	case "gguf":
		q.Set("filter", "gguf")
	default:
		q.Set("pipeline_tag", task)
	}
	var raw []hfRawModel
	if err := c.hfGet(ctx, "/api/models", q, &raw); err != nil {
		return nil, err
	}
	out := make([]HFModel, len(raw))
	for i, r := range raw {
		out[i] = r.summary()
	}
	return out, nil
}

// HFModelInfo returns a model's metadata and files with their sizes.
func (c *Colab) HFModelInfo(ctx context.Context, id string) (HFModelDetail, error) {
	if !hfRepoID.MatchString(id) {
		return HFModelDetail{}, fmt.Errorf("模型 ID 无效：%q", id)
	}
	var r hfRawModel
	if err := c.hfGet(ctx, "/api/models/"+id, url.Values{"blobs": {"true"}}, &r); err != nil {
		return HFModelDetail{}, err
	}
	d := HFModelDetail{HFModel: r.summary(), Files: []HFFile{}, Tags: r.Tags, DTypes: map[string]int64{}}
	if d.Tags == nil {
		d.Tags = []string{}
	}
	if s, ok := r.CardData.License.(string); ok {
		d.License = s
	}
	if r.Safetensors != nil {
		d.Parameters = r.Safetensors.Total
		for k, v := range r.Safetensors.Parameters {
			d.DTypes[k] = v
		}
	}
	for _, f := range r.Siblings {
		d.Files = append(d.Files, HFFile{Name: f.Name, Size: f.Size})
	}
	return d, nil
}

// HFTokenStatus tells whether a HuggingFace token is stored, and whose.
type HFTokenStatus struct {
	Set  bool   `json:"set"`
	User string `json:"user"`
}

// GetHFToken reports the stored token's owner, without the token.
func (c *Colab) GetHFToken(ctx context.Context) HFTokenStatus {
	tok, err := mygo.Secrets.Get(hfSecretKey)
	if err != nil || len(tok) == 0 {
		return HFTokenStatus{}
	}
	return HFTokenStatus{Set: true, User: hfWhoami(ctx, string(tok))}
}

// SetHFToken stores a token in the system's credential store; "" removes it.
func (c *Colab) SetHFToken(ctx context.Context, token string) (HFTokenStatus, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		err := mygo.Secrets.Delete(hfSecretKey)
		if errors.Is(err, mygo.ErrSecretNotFound) {
			err = nil
		}
		return HFTokenStatus{}, err
	}
	user := hfWhoami(ctx, token)
	if user == "" {
		return HFTokenStatus{}, errors.New("HuggingFace 不认这个 token")
	}
	if err := mygo.Secrets.Set(hfSecretKey, []byte(token)); err != nil {
		return HFTokenStatus{}, err
	}
	return HFTokenStatus{Set: true, User: user}, nil
}

func hfWhoami(ctx context.Context, token string) string {
	req, err := http.NewRequestWithContext(ctx, "GET", "https://huggingface.co/api/whoami-v2", nil)
	if err != nil {
		return ""
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := hfHTTP.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	var who struct {
		Name string `json:"name"`
	}
	if resp.StatusCode != http.StatusOK || json.NewDecoder(resp.Body).Decode(&who) != nil {
		return ""
	}
	return who.Name
}

// HFDownloadOptions say what to download, and where on the VM.
type HFDownloadOptions struct {
	Repo  string   `json:"repo"`
	Files []string `json:"files"`
	// Dest is a directory on the VM, e.g. /content/models/Qwen3-0.6B.
	Dest       string `json:"dest"`
	TotalBytes int64  `json:"totalBytes"`
}

// The token reaches the VM as an uploaded file, which the script moves into
// huggingface_hub's token path: code sent with colab exec is written to the
// session history, an upload only by its path.
const hfTokenUpload = "content/.colab_ui_hf_token"

const hfDownloadScript = `import os as _os, time as _time, threading as _th, shutil as _sh
_repo, _files, _dest, _total = %s, %s, %s, %d
_tok = "/` + hfTokenUpload + `"
if _os.path.exists(_tok):
    from huggingface_hub import constants as _hc
    _os.makedirs(_os.path.dirname(_hc.HF_TOKEN_PATH), exist_ok=True)
    _sh.move(_tok, _hc.HF_TOKEN_PATH)
    _os.chmod(_hc.HF_TOKEN_PATH, 0o600)
_os.environ["HF_HUB_DISABLE_PROGRESS_BARS"] = "1"
from huggingface_hub import hf_hub_download as _dl
from huggingface_hub.utils import disable_progress_bars as _npb
_npb()
_os.makedirs(_dest, exist_ok=True)
def _size():
    _n = 0
    for _r, _d, _fs in _os.walk(_dest):
        for _f in _fs:
            try: _n += _os.path.getsize(_os.path.join(_r, _f))
            except OSError: pass
    return _n
_base, _stop, _t0 = _size(), _th.Event(), _time.time()
def _report():
    while not _stop.wait(2):
        _n = max(0, _size() - _base); _s = _n / max(1e-6, _time.time() - _t0)
        _p = f" {100*_n/_total:.0f}%%" if _total else ""
        print(f"\r  {_n/1e9:.2f} / {_total/1e9:.2f} GB{_p} · {_s/1e6:.1f} MB/s", end="", flush=True)
_rt = _th.Thread(target=_report, daemon=True); _rt.start()
try:
    for _i, _f in enumerate(_files, 1):
        print(f"\n[{_i}/{len(_files)}] {_f}", flush=True)
        _dl(_repo, _f, local_dir=_dest)
finally:
    _stop.set(); _rt.join()
print(f"\n✓ {len(_files)} 个文件，用时 {_time.time()-_t0:.0f}s → {_dest}", flush=True)
del _os, _time, _th, _sh, _repo, _files, _dest, _total, _tok, _dl, _npb, _size, _base, _stop, _t0, _report, _rt
`

func pyLiteral(v any) string {
	// JSON strings and lists are valid Python literals.
	b, _ := json.Marshal(v)
	return string(b)
}

// HFDownload downloads files of a model onto the session's VM with
// huggingface_hub, streaming progress.
func (c *Colab) HFDownload(ctx context.Context, name string, opts HFDownloadOptions, out *mygo.Channel[Chunk]) error {
	if err := checkName(name); err != nil {
		return err
	}
	if !hfRepoID.MatchString(opts.Repo) {
		return fmt.Errorf("模型 ID 无效：%q", opts.Repo)
	}
	if len(opts.Files) == 0 {
		return errors.New("没有选择文件")
	}
	for _, f := range opts.Files {
		if !hfFileName.MatchString(f) || strings.HasPrefix(f, "/") || strings.Contains(f, "..") {
			return fmt.Errorf("文件名无效：%q", f)
		}
	}
	dest := path.Clean("/" + strings.ReplaceAll(opts.Dest, "\\", "/"))
	if dest == "/" {
		return errors.New("目标目录无效")
	}

	if tok, err := mygo.Secrets.Get(hfSecretKey); err == nil && len(tok) > 0 {
		if err := c.uploadSecret(ctx, name, tok); err != nil {
			return fmt.Errorf("上传 HuggingFace token 失败：%w", err)
		}
		_ = out.Send(Chunk{Stream: "info", Text: "已向 VM 提供 HuggingFace token\n"})
	}
	code := fmt.Sprintf(hfDownloadScript, pyLiteral(opts.Repo), pyLiteral(opts.Files), pyLiteral(dest), opts.TotalBytes)
	return c.stream(ctx, code, out, "exec", "--timeout", "0", "-s", name)
}

// uploadSecret puts the token on the VM as a file, removing the local copy
// at once.
func (c *Colab) uploadSecret(ctx context.Context, name string, tok []byte) error {
	f, err := os.CreateTemp("", "colab-ui-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if _, err := f.Write(tok); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	_, err = c.run(ctx, "", "upload", "-s", name, filepath.Clean(tmp), hfTokenUpload)
	return err
}
