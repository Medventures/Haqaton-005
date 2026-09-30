package main

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"testing"
)

var pngBytes = append([]byte("\x89PNG\r\n\x1a\n"), bytes.Repeat([]byte{0}, 64)...)

func (e *testEnv) upload(tok, dialogID, name string, data []byte) (int, map[string]json.RawMessage) {
	e.t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	if dialogID != "" {
		mw.WriteField("dialog_id", dialogID)
	}
	fw, _ := mw.CreateFormFile("file", name)
	fw.Write(data)
	mw.Close()
	req, _ := http.NewRequest("POST", e.srv.URL+"/api/chat/attachments", &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+tok)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer resp.Body.Close()
	out := map[string]json.RawMessage{}
	json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

func (e *testEnv) download(tok, id string) (int, []byte, string) {
	req, _ := http.NewRequest("GET", e.srv.URL+"/api/attachments/"+id, nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, b, resp.Header.Get("Content-Type")
}

func TestAttachmentUploadAndAccess(t *testing.T) {
	e := setup(t, ex("operator", "", false, "", "green"))
	owner, other, op := e.patient(), e.patient(), e.operator()

	code, out := e.upload(owner, "", "анализ крови.png", pngBytes)
	if code != 200 {
		t.Fatalf("upload: %d %s", code, out)
	}
	var dialogID string
	json.Unmarshal(out["dialog_id"], &dialogID)
	var msg Message
	json.Unmarshal(out["message"], &msg)
	var data struct {
		Attachments []Attachment `json:"attachments"`
	}
	json.Unmarshal(msg.Data, &data)
	if len(data.Attachments) != 1 || data.Attachments[0].ContentType != "image/png" || data.Attachments[0].Name != "анализ крови.png" {
		t.Fatalf("attachment meta: %s", msg.Data)
	}
	id := data.Attachments[0].ID

	if c, b, ct := e.download(owner, id); c != 200 || !bytes.Equal(b, pngBytes) || ct != "image/png" {
		t.Fatalf("owner download: %d %s", c, ct)
	}
	if c, _, _ := e.download(other, id); c != 404 {
		t.Fatalf("another patient must not get the file: %d", c)
	}
	if c, _, _ := e.download(op, id); c != 404 {
		t.Fatalf("operator before handoff: %d", c)
	}
	e.chat(owner, dialogID, "соедините с оператором")
	if c, _, _ := e.download(op, id); c != 200 {
		t.Fatalf("operator after handoff: %d", c)
	}
	if n, _ := e.llm.calls(); n != 1 {
		t.Fatalf("the upload itself must not call the LLM, extraction calls = %d", n)
	}
}

func TestAttachmentValidation(t *testing.T) {
	e := setup(t)
	tok := e.patient()
	if c, _ := e.upload(tok, "", "notes.txt", []byte("plain text, not an image")); c != 415 {
		t.Fatalf("text file: %d", c)
	}
	if c, _ := e.upload(tok, "", "fake.png", []byte("<html>not a png</html>")); c != 415 {
		t.Fatalf("type is checked by content, not by name: %d", c)
	}
	big := append(append([]byte{}, pngBytes...), make([]byte, maxAttachment)...)
	if c, _ := e.upload(tok, "", "big.png", big); c != 413 {
		t.Fatalf("over 10 MB: %d", c)
	}
	if c, _ := e.upload(e.patient(), "00000000-0000-0000-0000-000000000000", "a.png", pngBytes); c != 404 {
		t.Fatalf("unknown dialog: %d", c)
	}
	if c, _ := e.upload(e.operator(), "", "a.png", pngBytes); c != 403 {
		t.Fatalf("operators do not upload as patients: %d", c)
	}
}
