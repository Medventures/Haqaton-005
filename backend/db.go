package main

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

const schema = `
create table if not exists dialogs (
  id uuid primary key default gen_random_uuid(),
  patient_id text not null,
  status text not null default 'bot',          -- bot | operator
  language text not null default 'ru',
  urgency text not null default 'green',       -- green | yellow | red, only goes up
  urgency_reason text not null default '',
  clarifications int not null default 0,
  summary text not null default '',
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now()
);
alter table dialogs add column if not exists pregnant boolean not null default false;  -- set once, never cleared
alter table dialogs add column if not exists gestation_weeks int;
alter table dialogs add column if not exists pregnancy_asked boolean not null default false;
alter table dialogs add column if not exists assessed boolean not null default false;  -- urgency shown to the patient
alter table dialogs add column if not exists risk_factors text[] not null default '{}';  -- obstetric risk factors, only added
create table if not exists messages (
  id bigserial primary key,
  dialog_id uuid not null references dialogs(id) on delete cascade,
  role text not null,                          -- patient | bot | operator
  author text not null default '',
  content text not null,
  data jsonb not null default '{}',
  created_at timestamptz not null default now()
);
create index if not exists messages_dialog on messages(dialog_id, id);
create table if not exists attachments (
  id uuid primary key default gen_random_uuid(),
  dialog_id uuid not null references dialogs(id) on delete cascade,
  name text not null,
  content_type text not null,
  size int not null,
  data bytea not null,
  created_at timestamptz not null default now()
);
create table if not exists tickets (
  id bigserial primary key,
  dialog_id uuid not null references dialogs(id) on delete cascade,
  reason text not null,
  summary text not null,
  status text not null default 'open',         -- open | closed
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now()
);
`

func connectDB(ctx context.Context, url string) (*pgxpool.Pool, error) {
	var err error
	for i := 0; i < 30; i++ {
		var db *pgxpool.Pool
		if db, err = pgxpool.New(ctx, url); err == nil {
			if err = db.Ping(ctx); err == nil {
				_, err = db.Exec(ctx, schema)
				return db, err
			}
			db.Close()
		}
		time.Sleep(time.Second)
	}
	return nil, err
}

type Dialog struct {
	ID             string    `json:"id"`
	PatientID      string    `json:"patient_id"`
	Status         string    `json:"status"`
	Language       string    `json:"language"`
	Urgency        string    `json:"urgency"`
	UrgencyReason  string    `json:"urgency_reason"`
	Clarifications int       `json:"clarifications"`
	Summary        string    `json:"summary"`
	Pregnant       bool      `json:"pregnant"`
	GestationWeeks *int      `json:"gestation_weeks"`
	PregnancyAsked bool      `json:"pregnancy_asked"`
	Assessed       bool      `json:"assessed"`
	RiskFactors    []string  `json:"risk_factors"`
	CreatedAt      time.Time `json:"created_at"`
}

type Message struct {
	ID        int64           `json:"id"`
	Role      string          `json:"role"`
	Author    string          `json:"author"`
	Content   string          `json:"content"`
	Data      json.RawMessage `json:"data"`
	CreatedAt time.Time       `json:"created_at"`
}

type Ticket struct {
	ID        int64     `json:"id"`
	DialogID  string    `json:"dialog_id"`
	Reason    string    `json:"reason"`
	Summary   string    `json:"summary"`
	Status    string    `json:"status"`
	Urgency   string    `json:"urgency"`
	Language  string    `json:"language"`
	Pregnant  bool      `json:"pregnant"`
	Weeks     *int      `json:"gestation_weeks"`
	Risks     []string  `json:"risk_factors"`
	LastMsg   string    `json:"last_message"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (a *App) getDialog(ctx context.Context, id string) (*Dialog, error) {
	d := &Dialog{}
	err := a.db.QueryRow(ctx, `select id, patient_id, status, language, urgency, urgency_reason, clarifications, summary, pregnant, gestation_weeks, pregnancy_asked, assessed, risk_factors, created_at
		from dialogs where id=$1`, id).Scan(&d.ID, &d.PatientID, &d.Status, &d.Language, &d.Urgency, &d.UrgencyReason, &d.Clarifications, &d.Summary,
		&d.Pregnant, &d.GestationWeeks, &d.PregnancyAsked, &d.Assessed, &d.RiskFactors, &d.CreatedAt)
	return d, err
}

func (a *App) saveDialog(ctx context.Context, d *Dialog) error {
	_, err := a.db.Exec(ctx, `update dialogs set status=$2, language=$3, urgency=$4, urgency_reason=$5, clarifications=$6, summary=$7,
		pregnant = pregnant or $8, gestation_weeks=coalesce($9, gestation_weeks), pregnancy_asked = pregnancy_asked or $10,
		assessed = assessed or $11,
		risk_factors = array(select distinct unnest(risk_factors || $12::text[])), updated_at=now()
		where id=$1`, d.ID, d.Status, d.Language, d.Urgency, d.UrgencyReason, d.Clarifications, d.Summary, d.Pregnant, d.GestationWeeks, d.PregnancyAsked, d.Assessed, d.RiskFactors)
	return err
}

func (a *App) addMessage(ctx context.Context, dialogID, role, author, content string, data any) (*Message, error) {
	if data == nil {
		data = map[string]any{}
	}
	b, _ := json.Marshal(data)
	m := &Message{Role: role, Author: author, Content: content, Data: b}
	err := a.db.QueryRow(ctx, `insert into messages(dialog_id, role, author, content, data) values($1,$2,$3,$4,$5) returning id, created_at`,
		dialogID, role, author, content, b).Scan(&m.ID, &m.CreatedAt)
	return m, err
}

func (a *App) messages(ctx context.Context, dialogID string) ([]Message, error) {
	rows, err := a.db.Query(ctx, `select id, role, author, content, data, created_at from messages where dialog_id=$1 order by id`, dialogID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Message{}
	for rows.Next() {
		var m Message
		if err := rows.Scan(&m.ID, &m.Role, &m.Author, &m.Content, &m.Data, &m.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// handoff puts the dialog into the operator queue (one open ticket per dialog).
func (a *App) handoff(ctx context.Context, d *Dialog, reason string) (int64, error) {
	d.Status = "operator"
	d.Assessed = true // no more bot questions: show the level
	if err := a.saveDialog(ctx, d); err != nil {
		return 0, err
	}
	summary := d.Summary
	if summary == "" {
		summary = "Пациент ещё не описал запрос."
	}
	if len(d.RiskFactors) > 0 {
		names := []string{}
		for _, r := range d.RiskFactors {
			names = append(names, riskFactorNames[r])
		}
		summary += " Факторы риска: " + strings.Join(names, ", ") + "."
	}
	var id int64
	err := a.db.QueryRow(ctx, `update tickets set reason=$2, summary=$3, updated_at=now() where dialog_id=$1 and status='open' returning id`,
		d.ID, reason, summary).Scan(&id)
	if err == nil {
		return id, nil
	}
	err = a.db.QueryRow(ctx, `insert into tickets(dialog_id, reason, summary) values($1,$2,$3) returning id`, d.ID, reason, summary).Scan(&id)
	return id, err
}

const ticketCols = `t.id, t.dialog_id, t.reason, t.summary, t.status, d.urgency, d.language, d.pregnant, d.gestation_weeks, d.risk_factors,
	coalesce((select content from messages m where m.dialog_id=t.dialog_id order by id desc limit 1), ''), t.created_at, t.updated_at`

func scanTickets(rows interface {
	Next() bool
	Scan(...any) error
	Err() error
	Close()
}) ([]Ticket, error) {
	defer rows.Close()
	out := []Ticket{}
	for rows.Next() {
		var t Ticket
		if err := rows.Scan(&t.ID, &t.DialogID, &t.Reason, &t.Summary, &t.Status, &t.Urgency, &t.Language, &t.Pregnant, &t.Weeks, &t.Risks, &t.LastMsg, &t.CreatedAt, &t.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// VisibleUrgency is what the patient sees: empty until the dialog is assessed (red is assessed at once).
func (d *Dialog) VisibleUrgency() string {
	if !d.Assessed {
		return ""
	}
	return d.Urgency
}
