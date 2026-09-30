package main

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"

	"github.com/go-chi/chi/v5"
)

// Photos / PDFs of test results and ultrasound reports. They are stored in Postgres (inside the clinic
// perimeter) and shown to the operator/doctor; the bot does not read them.
const maxAttachment = 10 << 20

var allowedAttachment = map[string]bool{"image/jpeg": true, "image/png": true, "image/webp": true, "application/pdf": true}

type Attachment struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	ContentType string `json:"content_type"`
	Size        int    `json:"size"`
}

var attachmentReply = map[string]string{
	"ru": "Файл получен и сохранён в вашем диалоге — его посмотрит врач или оператор клиники. Бот не расшифровывает анализы и снимки. Если что-то беспокоит, опишите это словами.",
	"kk": "Файл алынды және диалогыңызда сақталды — оны клиниканың дәрігері немесе операторы қарайды. Бот анализдер мен суреттерді талдамайды. Бір нәрсе мазаласа, сөзбен жазыңыз.",
	"en": "The file has been received and saved in your conversation — a clinic doctor or operator will look at it. The bot does not interpret test results or scans. If something worries you, please describe it in words.",
}

// POST /api/chat/attachments (multipart: file, dialog_id?)
func (a *App) uploadAttachment(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u := userFrom(r)
	r.Body = http.MaxBytesReader(w, r.Body, maxAttachment+(1<<20))
	if err := r.ParseMultipartForm(1 << 20); err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			writeErr(w, 413, "file is larger than 10 MB")
			return
		}
		writeErr(w, 400, "multipart form with a file is required")
		return
	}
	f, hdr, err := r.FormFile("file")
	if err != nil {
		writeErr(w, 400, "file is required")
		return
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxAttachment+1))
	if err != nil {
		writeErr(w, 400, "cannot read file")
		return
	}
	if len(data) > maxAttachment {
		writeErr(w, 413, "file is larger than 10 MB")
		return
	}
	ctype := http.DetectContentType(data) // by content, not by the name
	if i := strings.Index(ctype, ";"); i >= 0 {
		ctype = ctype[:i]
	}
	if !allowedAttachment[ctype] {
		writeErr(w, 415, "only JPG, PNG, WEBP or PDF")
		return
	}
	name := []rune(filepath.Base(strings.ReplaceAll(hdr.Filename, "\\", "/")))
	if len(name) > 100 {
		name = name[len(name)-100:]
	}

	var d *Dialog
	if id := r.FormValue("dialog_id"); id != "" {
		if d, err = a.getDialog(ctx, id); err != nil || d.PatientID != u.ID {
			writeErr(w, 404, "dialog not found")
			return
		}
	} else {
		var id string
		if err = a.db.QueryRow(ctx, `insert into dialogs(patient_id) values($1) returning id`, u.ID).Scan(&id); err == nil {
			d, err = a.getDialog(ctx, id)
		}
		if err != nil {
			writeErr(w, 500, "db error")
			return
		}
	}
	att := Attachment{Name: string(name), ContentType: ctype, Size: len(data)}
	if err := a.db.QueryRow(ctx, `insert into attachments(dialog_id, name, content_type, size, data) values($1,$2,$3,$4,$5) returning id`,
		d.ID, att.Name, att.ContentType, att.Size, data).Scan(&att.ID); err != nil {
		writeErr(w, 500, "db error")
		return
	}
	msg, err := a.addMessage(ctx, d.ID, "patient", u.ID, att.Name, map[string]any{"attachments": []Attachment{att}})
	if err != nil {
		writeErr(w, 500, "db error")
		return
	}
	reply, err := a.addMessage(ctx, d.ID, "bot", "bot", t2(attachmentReply, d.Language),
		botData{Urgency: d.VisibleUrgency(), Actions: []string{"contact_operator"}})
	if err != nil {
		writeErr(w, 500, "db error")
		return
	}
	writeJSON(w, 200, map[string]any{"dialog_id": d.ID, "status": d.Status, "message": msg, "reply": reply})
}

// GET /api/attachments/{id}: the dialog's patient, or an operator once the dialog was handed off.
func (a *App) getAttachment(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u := userFrom(r)
	var dialogID, name, ctype string
	var data []byte
	if err := a.db.QueryRow(ctx, `select dialog_id, name, content_type, data from attachments where id=$1`, chi.URLParam(r, "id")).
		Scan(&dialogID, &name, &ctype, &data); err != nil {
		writeErr(w, 404, "not found")
		return
	}
	d, err := a.getDialog(ctx, dialogID)
	if err != nil {
		writeErr(w, 404, "not found")
		return
	}
	allowed := u.Role == "patient" && d.PatientID == u.ID
	if u.Role == "operator" {
		var n int
		a.db.QueryRow(ctx, `select count(*) from tickets where dialog_id=$1`, d.ID).Scan(&n)
		allowed = n > 0
	}
	if !allowed {
		writeErr(w, 404, "not found")
		return
	}
	w.Header().Set("Content-Type", ctype)
	w.Header().Set("Content-Disposition", fmt.Sprintf("inline; filename*=UTF-8''%s", url.PathEscape(name)))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "private, no-store")
	w.Write(data)
}

func t2(m map[string]string, lang string) string {
	if s := m[lang]; s != "" {
		return s
	}
	return m["ru"]
}
