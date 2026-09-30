#!/usr/bin/env python3
"""Live evaluation of the chat bot against a running backend with the real LLM (stdlib only).

  python3 live_eval.py run     [--base URL] [--only c01,y02] [--out live_results.jsonl]
  python3 live_eval.py dialogs [--base URL] [--out live_dialogs.jsonl]      # needs ASK_FIRST=true
  python3 live_eval.py report  [--results live_results.jsonl] [--after live_results_after.jsonl]

Cases run strictly one by one: the LM Studio behind the backend is shared with the live demo.
Each case gets a fresh patient token and a new dialog.
"""
import argparse
import json
import os
import statistics
import sys
import time
import unicodedata
import urllib.error
import urllib.request

HERE = os.path.dirname(os.path.abspath(__file__))
ROOT = os.path.dirname(os.path.dirname(HERE))
KZ_LETTERS = set("әғқңөұүһіӘҒҚҢӨҰҮҺІ")


def post(base, path, body=None, token=None, timeout=240):
    data = json.dumps(body or {}).encode()
    req = urllib.request.Request(base + path, data=data, method="POST", headers={"Content-Type": "application/json"})
    if token:
        req.add_header("Authorization", "Bearer " + token)
    try:
        with urllib.request.urlopen(req, timeout=timeout) as r:
            return r.status, json.loads(r.read() or b"{}")
    except urllib.error.HTTPError as e:
        try:
            return e.code, json.loads(e.read() or b"{}")
        except ValueError:
            return e.code, {}


def load_json(name):
    with open(os.path.join(ROOT, name), encoding="utf-8") as f:
        return json.load(f)


def strip_names(text):
    """Doctor and catalog names contain Kazakh letters («Ерланқызы») even in a Russian answer."""
    cat = load_json("catalog.json")
    for d in cat["doctors"]:
        for part in d["name"].split():
            text = text.replace(part, " ")
    return text


def reply_lang(text):
    """Language of the reply text: kk / en / ru / mixed (heuristic)."""
    t = strip_names(text or "")
    kz = sum(1 for ch in t if ch in KZ_LETTERS)
    lat = sum(1 for ch in t if "LATIN" in unicodedata.name(ch, "") and ch.isalpha())
    cyr = sum(1 for ch in t if "CYRILLIC" in unicodedata.name(ch, "") and ch.isalpha())
    if lat > cyr:
        return "en"
    if cyr == 0:
        return "?"
    words = [w.strip(".,!?;:()«»—-").lower() for w in t.split()]
    ru_words = sum(1 for w in words if w in {"и", "в", "не", "на", "вам", "вас", "что", "это", "для", "или", "по", "к", "с", "врачу", "есть", "если"})
    if kz >= 3:
        return "mixed" if ru_words >= 4 else "kk"
    return "ru"


def kb_match(reply):
    """Id of the knowledge-base entry whose answer is in the reply (any language), else None."""
    if not reply:
        return None
    for e in load_json("knowledge.json")["entries"]:
        for a in e["answer"].values():
            if a and a.strip() in reply:
                return e["id"]
    return None


def run_case(base, c):
    st, tok = post(base, "/api/auth/patient")
    if st != 200:
        return {"id": c["id"], "http": st, "error": "auth"}
    t0 = time.time()
    st, r = post(base, "/api/chat", {"message": c["text"]}, tok["token"])
    dt = time.time() - t0
    reply = (r.get("reply") or {}).get("content", "") if isinstance(r, dict) else ""
    svcs = r.get("services") or []
    return {
        "id": c["id"], "http": st, "latency_s": round(dt, 1),
        "urgency": r.get("urgency"), "language": r.get("language"), "status": r.get("status"),
        "specialty": svcs[0]["specialty_id"] if svcs else None,
        "services": [s["id"] for s in svcs], "actions": r.get("actions"), "ticket_id": r.get("ticket_id"),
        "reply": reply, "reply_lang": reply_lang(reply), "kb": kb_match(reply), "dialog_id": r.get("dialog_id"),
    }


def cmd_run(a):
    cases = json.load(open(a.cases, encoding="utf-8"))
    if a.only:
        ids = set(a.only.split(","))
        cases = [c for c in cases if c["id"] in ids]
    with open(a.out, "a" if a.append else "w", encoding="utf-8") as out:
        for i, c in enumerate(cases, 1):
            res = run_case(a.base, c)
            if res.get("http") != 200:  # one retry (tunnel hiccup)
                time.sleep(5)
                res = run_case(a.base, c)
            out.write(json.dumps(res, ensure_ascii=False) + "\n")
            out.flush()
            g = grade(c, res)
            bad = [k for k, v in g.items() if v is False]
            print(f"[{i}/{len(cases)}] {c['id']} {res.get('latency_s')}s urg={res.get('urgency')} spec={res.get('specialty')} "
                  f"lang={res.get('language')}/{res.get('reply_lang')} kb={res.get('kb')} {'FAIL ' + ','.join(bad) if bad else 'ok'}",
                  flush=True)


def expected_lang(c):
    return c.get("expect_lang") or c["lang"]


def grade(c, r):
    """Checks per case: True/False, or None when not applicable."""
    g = {"http": r.get("http") == 200}
    el = expected_lang(c)
    g["lang"] = r.get("language") == el
    g["reply_lang"] = r.get("reply_lang") == el if r.get("reply") else None
    g["specialty"] = (r.get("specialty") == c["expect_specialty"]) if c.get("expect_specialty") else None
    if c.get("expect_urgency"):
        g["urgency"] = r.get("urgency") == c["expect_urgency"]
    elif c["cat"] in ("greeting", "kb", "direct", "complaint"):
        g["urgency"] = r.get("urgency") != "red"  # nothing here is an emergency
    else:
        g["urgency"] = None
    g["kb"] = (r.get("kb") == c["expect_kb"]) if c.get("expect_kb") else None
    if c["cat"] == "greeting":
        g["no_services"] = not r.get("services")
    return g


def load_results(path):
    out = {}
    if path and os.path.exists(path):
        for line in open(path, encoding="utf-8"):
            if line.strip():
                r = json.loads(line)
                out[r["id"]] = r
    return out


def pct(ok, n):
    return f"{ok}/{n} ({100 * ok / n:.0f}%)" if n else "—"


def cmd_report(a):
    cases = json.load(open(a.cases, encoding="utf-8"))
    res = load_results(a.results)
    llm_err = set(filter(None, a.llm_errors.split(",")))
    if llm_err:  # infrastructure failures are not model mistakes: excluded from the metrics, listed separately
        print("Исключены из метрик (ошибка LLM, контекст): " + ", ".join(sorted(llm_err)) + "\n")
        res = {k: v for k, v in res.items() if k not in llm_err}
    after = load_results(a.after)
    merged = {**res, **after}  # after the fixes: re-run results replace the originals
    rows = []
    for title, pick in [
        ("Специальность (жалобы, жёлтые, прямые запросы)", lambda c, g: g["specialty"]),
        ("  — жалобы ru", lambda c, g: g["specialty"] if c["cat"] == "complaint" and c["lang"] == "ru" else None),
        ("  — жалобы kk", lambda c, g: g["specialty"] if c["cat"] == "complaint" and c["lang"] == "kk" else None),
        ("  — жалобы mix", lambda c, g: g["specialty"] if c["cat"] == "complaint" and c["lang"] == "mix" else None),
        ("  — жалобы en", lambda c, g: g["specialty"] if c["cat"] == "complaint" and c["lang"] == "en" else None),
        ("  — жёлтые", lambda c, g: g["specialty"] if c["cat"] == "yellow" else None),
        ("  — прямые запросы", lambda c, g: g["specialty"] if c["cat"] == "direct" else None),
        ("Red (15 кейсов) = red", lambda c, g: g["urgency"] if c["cat"] == "red" else None),
        ("Yellow (15 кейсов) = yellow", lambda c, g: g["urgency"] if c["cat"] == "yellow" else None),
        ("Срочность green там, где ожидается", lambda c, g: g["urgency"] if c.get("expect_urgency") == "green" else None),
        ("Нет ложного red (жалобы/KB/прямые/приветствия)", lambda c, g: g["urgency"] if c["cat"] in ("complaint", "kb", "direct", "greeting") else None),
        ("Язык: поле language", lambda c, g: g["lang"]),
        ("Язык: текст ответа", lambda c, g: g["reply_lang"]),
        ("  — текст ответа, kk и mix", lambda c, g: g["reply_lang"] if expected_lang(c) == "kk" else None),
        ("База знаний: нужная статья", lambda c, g: g["kb"]),
        ("Приветствия/не по теме: без услуг", lambda c, g: g.get("no_services")),
        ("HTTP 200", lambda c, g: g["http"]),
    ]:
        cols = []
        for rs in (res, merged) if after else (res,):
            ok = n = 0
            for c in cases:
                if c["id"] not in rs:
                    continue
                v = pick(c, grade(c, rs[c["id"]]))
                if v is not None:
                    n += 1
                    ok += bool(v)
            cols.append(pct(ok, n))
        rows.append((title, cols))
    head = "| Метрика | До правок |" + (" После правок |" if after else "")
    print(head)
    print("|---|---|" + ("---|" if after else ""))
    for title, cols in rows:
        print(f"| {title} | " + " | ".join(cols) + " |")
    lat = [r["latency_s"] for r in res.values() if r.get("latency_s") is not None]
    if lat:
        q = statistics.quantiles(lat, n=10)
        print(f"\nЗадержка (n={len(lat)}): p50 {statistics.median(lat):.1f} с, p90 {q[8]:.1f} с, max {max(lat):.1f} с")
    print("\n## Провалы\n")
    for c in cases:
        r = res.get(c["id"])
        if not r:
            continue
        g = grade(c, r)
        bad = [k for k, v in g.items() if v is False]
        if not bad:
            continue
        fix = ""
        if after and c["id"] in after:
            g2 = grade(c, after[c["id"]])
            bad2 = [k for k, v in g2.items() if v is False]
            fix = " → после правок: " + ("OK" if not bad2 else "всё ещё " + ", ".join(bad2)) + \
                  f" (spec={after[c['id']].get('specialty')}, urg={after[c['id']].get('urgency')}, kb={after[c['id']].get('kb')})"
        print(f"- **{c['id']}** ({c['cat']}, {c['lang']}) «{c['text']}» — не прошло: {', '.join(bad)}; "
              f"ожидали spec={c.get('expect_specialty')}, urg={c.get('expect_urgency')}, kb={c.get('expect_kb')}, lang={expected_lang(c)}; "
              f"получили spec={r.get('specialty')}, urg={r.get('urgency')}, kb={r.get('kb')}, lang={r.get('language')}/{r.get('reply_lang')}, "
              f"status={r.get('status')}{fix}")
        print(f"  > {(r.get('reply') or '(null)').replace(chr(10), ' ')[:400]}")


# Clarifying-question dialogs (ASK_FIRST=true): first message + short answers in the patient's language.
DIALOGS = [
    {"id": "q01", "lang": "ru", "expect": "ent", "turns": ["Болит горло", "Второй день, на 5 из 10", "Температура 37,2, небольшой насморк", "Глотать больно, налёта не видел", "Нет"]},
    {"id": "q02", "lang": "ru", "expect": "gastroenterologist", "turns": ["Болит живот после еды", "Вверху, под рёбрами", "Тошнит иногда, стул нормальный", "Да, после жирного, температуры нет", "Нет"]},
    {"id": "q03", "lang": "ru", "expect": "neurologist", "turns": ["Часто болит голова", "Температуры нет, давление нормальное", "Тошноты нет, зрение нормальное", "Травмы не было, болит уже месяц", "Нет"]},
    {"id": "q04", "lang": "kk", "expect": "ent", "turns": ["Тамағым ауырады", "Қызуым 37,4", "Жөтел жоқ, аздап тұмау бар", "Иә, жұтынғанда ауырады", "Жоқ"]},
    {"id": "q05", "lang": "kk", "expect": "dermatologist", "turns": ["Денеме бөртпе шықты", "Қолымда, кеше пайда болды", "Қышиды, қызу жоқ", "Жаңа крем қолдандым", "Жоқ"]},
    {"id": "q06", "lang": "kk", "expect": "neurologist", "turns": ["Белім ауырады", "Ауыр зат көтергеннен кейін", "Иә, аяғыма тарайды", "Қызу жоқ, зәр шығарғанда ауырмайды", "Жоқ"]},
    {"id": "q07", "lang": "kk", "expect": "gastroenterologist", "turns": ["Ішім ауырады", "Жоғарғы жағы ауырады", "Жүрек айниды, құсу жоқ", "Тамақ ішкеннен кейін, қызу жоқ", "Жоқ"]},
    {"id": "q08", "lang": "en", "expect": "ent", "turns": ["I have a sore throat", "Since yesterday, about 4 out of 10", "A bit of runny nose, no fever", "Yes, it hurts to swallow", "No"]},
    {"id": "q09", "lang": "en", "expect": "dermatologist", "turns": ["I have an itchy rash on my arm", "On my forearm, two days ago", "Itching, no fever", "No new food or cosmetics", "No"]},
    {"id": "q10", "lang": "mix", "expect_lang": "kk", "expect": "pulmonologist", "turns": ["Бір айдан бері жөтел болит, кашель не проходит", "Қызу жоқ, иногда одышка", "Қақырық аздап бар", "Темекі шегемін", "Жоқ"]},
]


def cmd_dialogs(a):
    with open(a.out, "w", encoding="utf-8") as out:
        for d in DIALOGS:
            st, tok = post(a.base, "/api/auth/patient")
            tok = tok["token"]
            dialog_id, log = "", []
            final = None
            for i, msg in enumerate(d["turns"]):
                if i >= 5:  # first message + up to 4 answers
                    break
                t0 = time.time()
                st, r = post(a.base, "/api/chat", {"message": msg, "dialog_id": dialog_id}, tok)
                dt = round(time.time() - t0, 1)
                dialog_id = r.get("dialog_id", dialog_id)
                reply = (r.get("reply") or {}).get("content", "")
                svcs = r.get("services") or []
                log.append({"patient": msg, "bot": reply, "urgency": r.get("urgency"), "specialty": svcs[0]["specialty_id"] if svcs else None,
                            "latency_s": dt, "status": r.get("status")})
                final = {"specialty": svcs[0]["specialty_id"] if svcs else None, "language": r.get("language"),
                         "reply_lang": reply_lang(reply), "urgency": r.get("urgency"), "status": r.get("status"), "reply": reply}
                if svcs or r.get("status") == "operator" or r.get("urgency") == "red":
                    break
            el = d.get("expect_lang") or d["lang"]
            rec = {"id": d["id"], "lang": d["lang"], "expect": d["expect"], "turns": len(log), "final": final, "log": log,
                   "spec_ok": final["specialty"] == d["expect"], "lang_ok": final["language"] == el, "reply_lang_ok": final["reply_lang"] == el}
            out.write(json.dumps(rec, ensure_ascii=False) + "\n")
            out.flush()
            print(f"{d['id']} turns={len(log)} spec={final['specialty']} (want {d['expect']}) lang={final['language']}/{final['reply_lang']}", flush=True)


def main():
    p = argparse.ArgumentParser()
    p.add_argument("cmd", choices=["run", "dialogs", "report"])
    p.add_argument("--base", default="http://localhost:8097")
    p.add_argument("--cases", default=os.path.join(HERE, "live_cases.json"))
    p.add_argument("--out", default=None)
    p.add_argument("--only", default="")
    p.add_argument("--append", action="store_true")
    p.add_argument("--results", default=os.path.join(HERE, "live_results.jsonl"))
    p.add_argument("--after", default=None)
    p.add_argument("--llm-errors", default="", help="ids whose extraction failed in the server log (Context size, ...)")
    a = p.parse_args()
    if a.cmd == "run":
        a.out = a.out or os.path.join(HERE, "live_results.jsonl")
        cmd_run(a)
    elif a.cmd == "dialogs":
        a.out = a.out or os.path.join(HERE, "live_dialogs.jsonl")
        cmd_dialogs(a)
    else:
        cmd_report(a)


if __name__ == "__main__":
    sys.exit(main())
