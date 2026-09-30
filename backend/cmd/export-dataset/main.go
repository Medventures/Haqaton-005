// Command export-dataset dumps anonymized dialogs as chat fine-tuning JSONL
// (OpenAI / LLaMA "messages" format). See docs/FINETUNE.md.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/jackc/pgx/v5"
)

const systemPrompt = "Ты — ассистент контакт-центра клиники MedHub. Помогаешь пациенту найти нужную услугу и врача, " +
	"уточняешь симптомы и срочность. Никогда не ставишь диагноз и не назначаешь лечение или лекарства. " +
	"Цены называешь только из каталога клиники. При угрозе жизни направляешь на 103."

type chatMsg struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type rawMsg struct {
	Role    string // patient | bot | operator
	Content string
}

// buildMessages converts stored messages to user/assistant turns.
// Operator replies are gold answers: a bot reply to the same patient turn is
// replaced by (or ignored in favour of) the operator reply. Consecutive
// messages of the same side are merged; leading assistant greetings and a
// trailing unanswered patient message are dropped.
func buildMessages(msgs []rawMsg) []chatMsg {
	var out []chatMsg
	lastSrc := "" // source role of the last assistant message: bot | operator
	for _, m := range msgs {
		text := strings.TrimSpace(Anonymize(m.Content))
		if text == "" {
			continue
		}
		switch m.Role {
		case "patient":
			if n := len(out); n > 0 && out[n-1].Role == "user" {
				out[n-1].Content += "\n" + text
			} else {
				out = append(out, chatMsg{"user", text})
			}
		case "bot", "operator":
			n := len(out)
			if n == 0 {
				continue // greeting before the patient said anything
			}
			if out[n-1].Role != "assistant" {
				out = append(out, chatMsg{"assistant", text})
				lastSrc = m.Role
				continue
			}
			switch {
			case lastSrc == "bot" && m.Role == "operator":
				out[n-1].Content = text // operator overrides the bot
				lastSrc = "operator"
			case lastSrc == "operator" && m.Role == "bot":
				// keep the operator's gold answer
			default:
				out[n-1].Content += "\n" + text
			}
		}
	}
	if n := len(out); n > 0 && out[n-1].Role == "user" {
		out = out[:n-1]
	}
	return out
}

func main() {
	defDB := os.Getenv("DATABASE_URL")
	if defDB == "" {
		defDB = "postgres://clinic:clinic@localhost:55433/clinic?sslmode=disable"
	}
	dbURL := flag.String("db", defDB, "Postgres URL")
	outPath := flag.String("out", "dataset.jsonl", "output JSONL file")
	onlyOp := flag.Bool("only-operator", false, "only dialogs where an operator replied")
	flag.Parse()

	ctx := context.Background()
	conn, err := pgx.Connect(ctx, *dbURL)
	if err != nil {
		fmt.Fprintln(os.Stderr, "connect:", err)
		os.Exit(1)
	}
	defer conn.Close(ctx)

	q := `select d.id::text, m.role, m.content from dialogs d join messages m on m.dialog_id = d.id`
	if *onlyOp {
		q += ` where exists (select 1 from messages o where o.dialog_id = d.id and o.role = 'operator')`
	}
	q += ` order by d.created_at, d.id, m.id`
	rows, err := conn.Query(ctx, q)
	if err != nil {
		fmt.Fprintln(os.Stderr, "query:", err)
		os.Exit(1)
	}

	f, err := os.Create(*outPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "create:", err)
		os.Exit(1)
	}
	defer f.Close()
	w := bufio.NewWriter(f)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)

	var total, written, skipped, lines int
	var cur string
	var buf []rawMsg
	flush := func() error {
		if cur == "" {
			return nil
		}
		total++
		msgs := buildMessages(buf)
		if len(msgs) < 2 {
			skipped++
			return nil
		}
		rec := struct {
			Messages []chatMsg `json:"messages"`
		}{append([]chatMsg{{"system", systemPrompt}}, msgs...)}
		written++
		lines += len(msgs)
		return enc.Encode(rec)
	}
	for rows.Next() {
		var id string
		var m rawMsg
		if err := rows.Scan(&id, &m.Role, &m.Content); err != nil {
			fmt.Fprintln(os.Stderr, "scan:", err)
			os.Exit(1)
		}
		if id != cur {
			if err := flush(); err != nil {
				fmt.Fprintln(os.Stderr, "write:", err)
				os.Exit(1)
			}
			cur, buf = id, nil
		}
		buf = append(buf, m)
	}
	if err := rows.Err(); err != nil {
		fmt.Fprintln(os.Stderr, "rows:", err)
		os.Exit(1)
	}
	if err := flush(); err != nil {
		fmt.Fprintln(os.Stderr, "write:", err)
		os.Exit(1)
	}
	if err := w.Flush(); err != nil {
		fmt.Fprintln(os.Stderr, "flush:", err)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "dialogs read: %d, written: %d, skipped (<2 turns): %d, messages: %d -> %s\n",
		total, written, skipped, lines, *outPath)
}
