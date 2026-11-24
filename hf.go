package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
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

// The token reaches the VM as an uploaded file, which the download moves
// into huggingface_hub's token path: code sent with colab exec is written to
// the session history, an upload only by its path.
const hfTokenUpload = "content/.colab_ui_hf_token"

func pyLiteral(v any) string {
	// JSON strings and lists are valid Python literals.
	b, _ := json.Marshal(v)
	return string(b)
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
