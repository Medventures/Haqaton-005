#!/usr/bin/env python3
"""Generates cases.jsonl for TestThousandCases (backend/cases_test.go).

Deterministic: fixed seed, templates x vocab, plus handwritten tricky cases (HAND below).
Run from anywhere:  python3 backend/testdata/gen_cases.py

Line fields:
  text, cat, lang_hint (ru|kk|en|mix|translit),
  expect_red (bool), expect_pregnant (bool|null), expect_weeks (int|null),
  expect_kb ("<id>" = top entry must be this id; "" = no entry with score >= 2, i.e. a complaint
             must not be answered from the knowledge base; null = don't check),
  expect_lang ("ru" is written as "" = detectLanguage gives no verdict; "kk"; "en"; null = don't check),
  src (gen|hand), note.
Categories whose name starts with red_ru/red_kk/red_en are clean-text emergencies: the Go test requires 100%.
"""
import json
import os
import random

random.seed(20260930)
OUT = os.path.join(os.path.dirname(os.path.abspath(__file__)), "cases.jsonl")
KK_LETTERS = set("әғқңөұүһіӘҒҚҢӨҰҮҺІ")
cases = []


def has_kk(s):
    return any(c in KK_LETTERS for c in s)


def lang_for(hint, text):
    if hint == "ru":
        return ""
    if hint == "en":
        return "en"
    if hint == "kk":
        return "kk"
    if hint == "mix":
        return "kk" if has_kk(text) else None
    return None  # translit: see HAND cases with an explicit expectation


def add(cat, text, hint, red=False, preg=False, weeks=None, kb=None, lang="auto", note="", src="gen"):
    if lang == "auto":
        lang = lang_for(hint, text)
    cases.append({"text": text, "cat": cat, "lang_hint": hint, "expect_red": red,
                  "expect_pregnant": preg, "expect_weeks": weeks, "expect_kb": kb,
                  "expect_lang": lang, "src": src, "note": note})


def pick(seq, k):
    seq = list(seq)
    random.shuffle(seq)
    return seq[:k]


# Kazakh typed on a Russian keyboard: special letters replaced by look-alikes.
KK_FOLD = str.maketrans("әғқңөұүһіӘҒҚҢӨҰҮҺІ", "агкнөуухиАГКНӨУУХИ".replace("ө", "о").replace("Ө", "О"))


def no_kk(s):
    return s.translate(KK_FOLD)


# ---------------------------------------------------------------- 1. clean emergencies
RED_RU = [
    "болит грудь", "сильная боль в груди", "давит в груди", "жжёт в груди", "боль в грудной клетке, отдаёт в левую руку",
    "колет в груди и отдаёт в челюсть", "болит сердце", "сжимает грудь",
    "онемела половина лица", "перекосило лицо", "перекосило рот на одну сторону", "онемела левая сторона тела",
    "не могу говорить, язык не слушается", "речь невнятная, язык заплетается", "отнялась левая рука", "рука и нога отнялись справа",
    "трудно дышать", "задыхаюсь", "нечем дышать", "не могу дышать", "дышать тяжело", "не хватает воздуха", "синеют губы, задыхается",
    "сильное кровотечение", "кровь из раны не останавливается", "кровь из носа не останавливается уже час", "рвота с кровью", "рвёт кровью",
    "потерял сознание", "потеряла сознание", "упала в обморок", "сознание потеряла", "лежит без сознания", "не приходит в себя",
    "судороги", "начался эпилептический приступ", "припадок, трясёт всего",
    "думаю о суициде", "хочу покончить с собой", "не хочу жить", "думаю о самоубийстве", "хочу умереть", "наглоталась таблеток",
    "похоже на инсульт", "кажется инфаркт", "ребёнок не дышит",
]
PRE_RU = ["", "срочно ", "помогите, ", "мне плохо, ", "у меня ", "доктор, "]
SUF_RU = ["", " уже 20 минут", ", что делать?", "!!!", " помогите", " прямо сейчас", " со вчерашнего вечера", " пж"]
for s in RED_RU:
    combos = pick([(p, q) for p in PRE_RU for q in SUF_RU], 3)
    for p, q in combos:
        if p == "у меня " and s.split()[0] in ("потерял", "потеряла", "упала", "лежит", "думаю", "хочу", "не", "наглоталась", "ребёнок", "похоже", "кажется", "сознание", "задыхаюсь", "начался"):
            p = ""
        add("red_ru", (p + s + q).strip(), "ru", red=True)

RED_KK = [
    "кеудем қатты ауырып тұр", "кеудем ауырады", "кеуде тұсым қысып тұр", "жүрегім ауырып тұр", "жүрек тұсым қатты ауырады",
    "жүрегім шаншып тұр, сол қолыма береді", "кеудеме ауыр салмақ түскендей",
    "бетімнің жартысы ұйып қалды", "бетім қисайып кетті", "денемнің бір жағы ұйып қалды", "сөйлей алмай жатыр", "тілі күрмеліп қалды",
    "тыныс ала алмай жатырмын", "дем ала алмай жатыр", "демім жетпей тұр", "тынысым тарылып барады", "тұншығып жатырмын",
    "дем алу қиын", "ауа жетпей тұр", "ерні көгеріп кетті, тұншығып жатыр",
    "қан тоқтамай жатыр", "қатты қан кетіп жатыр", "мұрнымнан қан тоқтамай ағып жатыр", "қан құсып жатыр",
    "есінен танып қалды", "ес-түссіз жатыр", "естен танды", "талып қалды", "есін жоғалтты", "есі жоқ жатыр",
    "құрысып жатыр", "талмасы ұстап қалды",
    "өмір сүргім келмейді", "өзімді өлтіргім келеді", "өзіме қол жұмсағым келеді", "өлгім келеді",
    "инсульт болып жатыр", "бала дем алмай жатыр",
]
PRE_KK = ["", "тез көмектесіңіз, ", "шұғыл ", "дәрігер, ", "маған жаман, "]
SUF_KK = ["", " қазір", ", не істеймін?", "!!!", " көмектесіңіздерші", " 10 минут болды"]
for s in RED_KK:
    for p, q in pick([(p, q) for p in PRE_KK for q in SUF_KK], 2):
        add("red_kk", (p + s + q).strip(), "kk", red=True)

RED_EN = [
    "I have chest pain", "severe chest pain", "my chest hurts", "tightness in my chest", "pressure in my chest spreading to my arm",
    "my face is drooping on one side", "my face went numb", "slurred speech", "one side of my body is numb", "sudden weakness on one side",
    "I can't breathe", "I cannot breathe", "short of breath", "trouble breathing", "difficulty breathing", "can't catch my breath",
    "severe bleeding", "bleeding heavily", "the bleeding won't stop", "vomiting blood",
    "he passed out", "she fainted", "my dad is unconscious", "he is unresponsive", "I blacked out",
    "having a seizure", "my son has convulsions",
    "I want to kill myself", "I have suicidal thoughts", "I want to die", "I took an overdose",
    "I think it's a stroke", "I think I'm having a heart attack", "my baby is not breathing",
]
PRE_EN = ["", "please help, ", "urgent: ", "doctor, "]
SUF_EN = ["", " right now", ", what should I do?", "!!!", " for 20 minutes"]
for s in RED_EN:
    for p, q in pick([(p, q) for p in PRE_EN for q in SUF_EN], 2):
        add("red_en", (p + s + q).strip(), "en", red=True)

# ---------------------------------------------------------------- 2. mixed ru+kk emergencies
RED_MIX = [
    "кеудем болит", "кеудем қатты болит", "кеуде болит срочно", "грудь ауырып тұр", "груди ауырады", "грудь қысып тұр",
    "жүрегім болит", "сердце ауырып тұр", "сердце қатты ауырады", "дышать не могу, көмектесіңіз", "тыныс ала алмай жатыр, срочно",
    "задыхаюсь, көмектесіңдерші", "мама есінен танып қалды срочно", "сознание жоғалтты", "бетім онемел", "қан остановиться не может",
    "кровь тоқтамай жатыр", "судорога болып жатыр", "талып қалды, скорую шақырдық", "өмір сүргім келмейді уже",
    "не хочу жить, шаршадым", "инсульт болды кажется", "папам потерял сознание", "әкем задыхается",
    "балам не дышит", "кеудем давит", "кеудем жжёт",
]
PRE_MIX = ["", "срочно ", "көмектесіңіз ", "пж "]
SUF_MIX = ["", " уже", " 10 минут", " что делать", " не істейміз"]
for s in RED_MIX:
    for p, q in pick([(p, q) for p in PRE_MIX for q in SUF_MIX], 3):
        add("red_mix", (p + s + q).strip(), "mix", red=True)

# ---------------------------------------------------------------- 3. noisy: caps, no punctuation, no Kazakh letters, typos
NOISY = []
for s in pick(RED_RU, 22):
    NOISY.append((s.upper() + "!!!", "ru", "caps"))
for s in pick(RED_KK, 22):
    NOISY.append((no_kk(s), "kk", "kk typed without special letters"))
for s in pick(RED_KK, 8):
    NOISY.append((no_kk(s).upper(), "kk", "caps, no special letters"))
for s in pick(RED_MIX, 10):
    NOISY.append((no_kk(s) + " срочно пж", "mix", "mix without special letters"))
TYPOS = [
    ("балит грудь", "ru"), ("балит грудь сильно", "ru"), ("грудь балит", "ru"), ("задыхаюс", "ru"), ("задыхаюсь нечем дышат", "ru"),
    ("патеряла сознание", "ru"), ("сознание потерял", "ru"), ("патерял сознание", "ru"), ("не магу дышать", "ru"),
    ("кеудем ауырп тур", "kk"), ("тыныс ала алмай жатрмын", "kk"),
    ("ЖУРЕГИМ АУЫРЫП ТУР", "kk"), ("есинен танып калды", "kk"), ("талып калды", "kk"),
]
for t, h in TYPOS:
    NOISY.append((t, h, "typo"))
for t, h, note in NOISY:
    lang = "" if h == "ru" else ("kk" if h == "kk" and has_kk(t) else None)
    add("red_noisy", t, h, red=True, lang=lang, note=note)

# ---------------------------------------------------------------- 4. Latin transliteration
TRANSLIT = [
    ("bolit grud", "ru"), ("silno bolit grud", "ru"), ("bol v grudi", "ru"), ("davit v grudi", "ru"), ("ne mogu dyshat", "ru"),
    ("zadyhayus", "ru"), ("poteryal soznanie", "ru"), ("poteryala soznanie", "ru"), ("mama bez soznaniya", "ru"),
    ("kudem auyrady", "kk"), ("keudem katty auyryp tur", "kk"), ("tynys ala almai zhatyrmyn", "kk"), ("dem ala almai zhatyr", "kk"),
    ("esinen tanyp kaldy", "kk"), ("insult bolyp zhatyr", "kk"), ("infarkt kazhetsya", "ru"), ("sudorogi u rebenka", "ru"),
    ("ne hochu zhit", "ru"), ("BOLIT GRUD SROCHNO", "ru"), ("kudem bolit", "translit"),
]
for t, h in TRANSLIT:
    add("red_translit", t, "translit", red=True, lang=None, note="Latin transliteration of " + h)

# ---------------------------------------------------------------- 5. pregnancy + red_if_pregnant
PREG_INTRO = [
    ("я беременна", None, "ru"), ("я на 32 неделе", 32, "ru"), ("беременна, срок 20 недель", 20, "ru"), ("срок 36 недель", 36, "ru"),
    ("я в положении", None, "ru"), ("мен жүктімін", None, "kk"), ("жүктімін, 28 апта", 28, "kk"), ("32 аптадамын", 32, "kk"),
    ("жүктілік мерзімі 36 апта", 36, "kk"), ("екіқабатпын", None, "kk"),
    ("жүктімін 30 неделя", 30, "mix"), ("беременна 12 апта", 12, "mix"), ("берем 25 нед", 25, "mix"), ("бер-ть 34 нед", 34, "mix"),
    ("ж/ты 20 апт", 20, "mix"), ("беременна 22 нед", 22, "mix"),
    ("I'm 28 weeks pregnant", 28, "en"), ("I am pregnant", None, "en"), ("im 30w preg", 30, "en"),
]
PREG_RED = {
    "ru": ["тянет низ живота", "кровянистые выделения", "мажет кровью", "отошли воды", "подтекают воды", "ребёнок не шевелится с утра",
           "сильно болит голова и мушки перед глазами", "отекло лицо", "схватки каждые 5 минут", "упала на живот", "температура 39",
           "темп 39", "сильная боль в животе", "пошла кровь"],
    "kk": ["қан кетіп жатыр", "су кетті", "бала қимылдамай жатыр", "іштің төменгі жағы ауырады", "басым қатты ауырып тұр",
           "көз алдым қарауытып тұр", "бетім ісіп кетті", "толғақ басталды", "ішім қатты ауырып тұр", "құлап қалдым ішпен",
           "баланың қимылын сезбей жатырмын", "қызуым 39"],
    "en": ["some bleeding", "my water broke", "the baby is not moving", "contractions every 5 minutes", "severe headache",
           "blurred vision", "pain in my lower abdomen", "I fell on my belly"],
    "mix": ["ішім қатты болит", "низ живота ауырады", "қан идёт", "воды кетті", "ребёнок қимылдамай жатыр", "схватки басталды",
            "басым қатты болит", "кровь кетіп жатыр"],
}
for intro, weeks, ih in PREG_INTRO:
    pool = PREG_RED["en"] if ih == "en" else PREG_RED["ru"] + PREG_RED["kk"] + PREG_RED["mix"]
    for sym in pick(pool, 4 if ih == "en" else 5):
        sym_lang = next(k for k, v in PREG_RED.items() if sym in v)
        hint = ih if sym_lang == ih else "mix"
        cat = "red_preg" if hint != "mix" else "red_preg_mix"
        text = intro + ", " + sym
        add(cat, text, hint, red=True, preg=True, weeks=weeks)

# ---------------------------------------------------------------- 6. red_if_pregnant symptoms WITHOUT pregnancy: not red
for sym in pick(PREG_RED["ru"], 10) + pick(PREG_RED["kk"], 8) + pick(PREG_RED["en"], 5):
    if sym in ("упала на живот",):  # trauma: LLM
        continue
    h = "en" if sym in PREG_RED["en"] else ("kk" if has_kk(sym) else "ru")
    add("not_red_nopreg", sym, h, red=False)

# ---------------------------------------------------------------- 7. false-red traps
TRAPS = [
    # negation
    ("боли в груди нет", "ru"), ("грудь не болит, болит горло", "ru"), ("в груди не болит, просто кашель", "ru"),
    ("не задыхаюсь, просто насморк", "ru"), ("обморока не было, просто голова кружится", "ru"),
    ("судорог не было, температура 38", "ru"), ("сознание не терял, просто упал", "ru"), ("кровотечения нет", "ru"),
    ("кеудем ауырмайды, тамағым ауырады", "kk"), ("кеудемнің ауыруы жоқ", "kk"), ("кеудем ауырған жоқ", "kk"),
    ("no chest pain, just a cough", "en"), ("I don't have chest pain", "en"),
    # already passed
    ("боль в груди уже прошла", "ru"), ("боль в груди прошла", "ru"), ("грудь болела, но уже прошло", "ru"),
    ("the chest pain went away", "en"), ("кеудем ауырған, қазір басылды", "kk"),
    # history / past
    ("в прошлом году был инфаркт, хочу к кардиологу", "ru"), ("инфаркт был 3 года назад", "ru"), ("инфаркт миокарда в анамнезе", "ru"),
    ("после инсульта наблюдаюсь у невролога", "ru"), ("обморок был год назад", "ru"), ("в детстве были судороги", "ru"),
    ("месяц назад болела грудь", "ru"), ("хочу ЭКГ после инфаркта", "ru"), ("перенесла инсульт, нужен реабилитолог", "ru"),
    ("бір ай бұрын есінен танып қалды", "kk"), ("өткен жылы кеудем ауырған", "kk"), ("инфаркттан кейін тексерілу керек", "kk"),
    ("I had a stroke last year", "en"), ("history of heart attack, need a checkup", "en"), ("I fainted years ago", "en"),
    # relatives
    ("у папы был инфаркт", "ru"), ("бабушка перенесла инсульт", "ru"), ("у отца инфаркт был в 50 лет", "ru"),
    ("дедушка умер от инфаркта, хочу проверить сердце", "ru"), ("әкем инсульт алған", "kk"),
    ("атам инфаркттан қайтыс болған, жүрегімді тексергім келеді", "kk"), ("анам инсульт болған, кеңес керек", "kk"),
    ("my father had a heart attack", "en"),
    # fear / prevention
    ("боюсь инфаркта, хочу проверить сердце", "ru"), ("профилактика инсульта", "ru"), ("хочу проверить риск инфаркта", "ru"),
    # breast / infant / anatomy
    ("грудной ребёнок кашляет", "ru"), ("грудничок плохо спит", "ru"), ("грудное вскармливание и температура", "ru"),
    ("болит грудь, кормлю грудью", "ru"), ("болит грудь при кормлении", "ru"), ("кормящая мама, болит левая грудь", "ru"),
    ("лактостаз, болит грудь", "ru"), ("болит грудной отдел позвоночника", "ru"), ("остеохондроз грудного отдела, болит спина", "ru"),
    ("балам кеуде сүтін емеді, ауырып жатыр", "kk"), ("кеуде сүті азайып кетті", "kk"), ("емшегім ауырады, емізіп жүрмін", "kk"),
    ("breastfeeding and my breast hurts", "en"),
    # leg cramps are not seizures
    ("судороги в ногах по ночам", "ru"), ("сводит ноги судорогой", "ru"), ("судорога в икре после бега", "ru"),
    ("аяғым құрысып қалады түнде", "kk"), ("leg cramps at night", "en"),
    # ordinary complaints
    ("болит горло третий день", "ru"), ("температура 38 и кашель", "ru"), ("болит живот после еды", "ru"),
    ("кровь из носа пошла, уже остановилась", "ru"), ("давление высокое, голова кружится", "ru"),
    ("тамағым ауырады", "kk"), ("басым ауырады", "kk"), ("басым айналып тұр, давление высокое", "mix"),
    ("тамағым болит уже 3 күн", "mix"), ("ішім қатты болит", "mix"), ("I have a sore throat", "en"), ("my back hurts", "en"),
    ("сколько стоит ЭКГ?", "ru"), ("хочу к кардиологу, сердце проверить", "ru"), ("жүрегім айнып тұр", "kk"),
    ("аяғым ұйып қалды отырып", "kk"), ("рука немеет по ночам", "ru"),
]
for t, h in TRAPS:
    add("not_red", t, h, red=False)
# templated negations: symptom stems x negation frames
NEG_FRAMES_RU = ["{s} нет", "{s} нету", "никаких {s} нет", "{s} не было"]
NEG_STEMS_RU = ["болей в груди", "судорог", "обмороков", "кровотечения", "одышки"]
for f in NEG_FRAMES_RU:
    for s in NEG_STEMS_RU:
        if f == "никаких {s} нет" and s in ("кровотечения", "одышки"):
            continue
        add("not_red", f.format(s=s) + ", болит горло", "ru", red=False, note="negation frame")
HIST_RU = ["{r} был инфаркт", "{r} был инсульт", "{r} перенёс инфаркт", "{r} перенесла инсульт"]
REL_RU = ["у папы", "у мамы", "у дедушки", "у бабушки", "у брата", "в анамнезе"]
for f in HIST_RU:
    for r in pick(REL_RU, 4):
        text = f.format(r=r)
        if text.startswith("у ") and ("перенёс" in text or "перенесла" in text):
            text = text[2:].replace("папы", "папа").replace("мамы", "мама").replace("дедушки", "дедушка").replace("бабушки", "бабушка").replace("брата", "брат")
        add("not_red", text + ", хочу провериться", "ru", red=False, note="relatives/history")

# ---------------------------------------------------------------- 8. pregnancy detection
PREG_YES = [
    ("Я беременна", None, "ru"), ("я на 32 неделе", 32, "ru"), ("срок 12 недель", 12, "ru"), ("20 неделя беременности", 20, "ru"),
    ("я в положении", None, "ru"), ("жду ребёнка, 15 недель", 15, "ru"), ("у меня 30-я неделя, срок большой", 30, "ru"),
    ("Мен жүктімін", None, "kk"), ("32-аптадамын", 32, "kk"), ("жүктілік мерзімі 28 апта", 28, "kk"), ("екіқабатпын, 16 апталық", 16, "kk"),
    ("аяғым ауыр, 7 айлықпын", None, "kk"),
    ("жүктімін 20 неделя", 20, "mix"), ("беременна 12 апта", 12, "mix"), ("жүктімін, 30 неделя", 30, "mix"),
    ("берем 25 нед", 25, "mix"), ("бер-ть 34 нед", 34, "mix"), ("беременна 22 нед", 22, "mix"), ("ж/ты 20 апт", 20, "mix"),
    ("30 апта жүктімін", 30, "kk"), ("жукти 14 апта", 14, "mix"), ("ЖҮКТІМІН", None, "kk"), ("БЕРЕМЕННА 8 НЕДЕЛЬ", 8, "ru"),
    ("I'm 28 weeks pregnant", 28, "en"), ("I am pregnant", None, "en"), ("im 30w preg", 30, "en"), ("pregnant, 12 weeks", 12, "en"),
    ("beremenna 20 nedel", 20, "translit"), ("zhuktimin", None, "translit"),
]
FILL = ["", ", болит поясница", ", вопрос", ", хочу к гинекологу", " тошнит", ", отекают ноги", " помогите пж"]
for t, w, h in PREG_YES:
    for f in pick(FILL, 3):
        if h == "en" and f:
            f = random.choice([", my back hurts", ", question", " need a gynecologist"])
        text = t + f
        hint = h if not (f and h in ("kk", "en", "translit") and h != "en") else "mix"
        add("preg_detect", text, hint, preg=True, weeks=w, lang=None if h == "translit" else "auto")

PREG_NO = [
    "екі апта бойы тамағым ауырады", "болит горло уже неделю", "через 2 недели хочу записаться", "задержка 5 дней",
    "2 аптада бір рет басым ауырады", "уезжаю на 2 недели", "я не беременна", "тянет низ живота, беременности нет",
    "мен жүкті емеспін", "я не в положении", "планирую беременность, какие анализы сдать", "хочу забеременеть",
    "не могу забеременеть уже год", "тест на беременность отрицательный", "жүкті бола алмай жүрмін", "жүкті болғым келеді",
    "I'm not pregnant", "trying to get pregnant", "3 недели кашель", "30 неделя как болит спина",
    "беременности нет, болит живот", "жүктілік жоқ", "мы берем талон на завтра", "берем анализы у ребенка?",
    "5 апта болды жөтел", "2 нед температура", "температура держится неделю",
]
for t in PREG_NO:
    for f in pick(["", " пж", ", что делать"], 2):
        h = "kk" if has_kk(t) else ("en" if t[0].isascii() else "ru")
        add("preg_negative", t + f, h, preg=False, lang=None)

# ---------------------------------------------------------------- 9. knowledge base
KB_Q = {
    "address": [("Где вы находитесь?", "ru"), ("какой у вас адрес", "ru"), ("как к вам доехать", "ru"), ("как к вам проехать?", "ru"),
                ("Қай жерде орналасқансыздар?", "kk"), ("мекенжайыңыз қандай", "kk"), ("клиника қайда орналасқан", "kk"),
                ("адрес қандай", "mix"), ("адрес скиньте пж", "ru"), ("Where are you located?", "en"), ("what is your address", "en")],
    "hours": [("Во сколько открываетесь в субботу?", "ru"), ("режим работы", "ru"), ("работаете в воскресенье?", "ru"),
              ("до скольки работаете", "ru"), ("график работы клиники", "ru"), ("жұмыс уақыты қандай", "kk"),
              ("сағат нешеде ашыласыздар", "kk"), ("жексенбі күні жұмыс істейсіздер ме", "kk"), ("What are your opening hours?", "en"),
              ("are you open on weekends", "en")],
    "contacts": [("Какой у вас номер телефона?", "ru"), ("как с вами связаться", "ru"), ("телефон нөміріңіз қандай", "kk"),
                 ("What is your phone number?", "en")],
    "parking": [("Есть парковка?", "ru"), ("где припарковаться", "ru"), ("көлік қоятын тұрақ бар ма", "kk"), ("Is there parking?", "en")],
    "payment": [("Можно ли оплатить картой?", "ru"), ("оплата каспи есть?", "ru"), ("каспимен төлесем бола ма", "mix"),
                ("қолма-қол төлеуге бола ма", "kk"), ("Can I pay by card?", "en"), ("можно наличными оплатить", "ru")],
    "osms": [("Принимаете ОСМС?", "ru"), ("по ОСМС бесплатно?", "ru"), ("МӘМС бойынша тегін бе", "kk"), ("осмс бар ма", "mix"),
             ("Do you accept compulsory insurance?", "en")],
    "booking": [("Как записаться к врачу?", "ru"), ("хочу записаться на приём", "ru"), ("дәрігерге қалай жазылуға болады", "kk"),
                ("How do I book an appointment?", "en")],
    "reschedule_cancel": [("Как отменить запись?", "ru"), ("можно перенести запись на завтра", "ru"), ("жазылуды болдырмау керек", "kk"),
                          ("I need to cancel my appointment", "en")],
    "results": [("Когда будут готовы результаты анализов?", "ru"), ("анализ нәтижесі қашан дайын болады", "kk"),
                ("When will my test results be ready?", "en")],
    "prep_cbc": [("Как подготовиться к анализу крови?", "ru"), ("кровь сдавать натощак?", "ru"), ("Қан тапсыруға қалай дайындалу керек?", "kk"),
                 ("анализ крови аш қарынға тапсыру керек пе", "mix"), ("Do I need fasting for a blood test?", "en")],
    "prep_gastro_us": [("Как подготовиться к УЗИ брюшной полости?", "ru"), ("узи живота натощак делают?", "ru"),
                       ("іш қуысының УДЗ-на қалай дайындалу керек", "kk"), ("How to prepare for an abdominal ultrasound?", "en")],
    "prep_ecg": [("нужна ли подготовка к ЭКГ", "ru"), ("ЭКГ-ға дайындық керек пе", "kk"), ("Do I need to prepare for an ECG?", "en")],
    "prep_mri": [("можно ли МРТ с кардиостимулятором", "ru"), ("МРТ-ға қалай дайындалу керек", "kk"), ("Can I have an MRI with a pacemaker?", "en")],
    "children": [("Можно с ребенком прийти?", "ru"), ("принимаете детей?", "ru"), ("Do you see children?", "en")],
    "home_visit": [("можно вызвать врача на дом", "ru"), ("дәрігерді үйге шақыруға бола ма", "kk"), ("Do you do home visits?", "en")],
    "certificates": [("нужна справка для работы", "ru"), ("больничный лист даёте?", "ru"), ("анықтама алуға бола ма", "kk")],
    "documents": [("какие документы взять с собой", "ru"), ("қандай құжат алу керек", "kk"), ("What documents should I bring?", "en")],
    "languages": [("врачи говорят по-казахски?", "ru"), ("Do your doctors speak English?", "en")],
    "dms": [("работаете с ДМС?", "ru"), ("страховой полис принимаете", "ru")],
    "accessibility": [("есть пандус для коляски?", "ru"), ("Is the clinic wheelchair accessible?", "en")],
}
for kb, qs in KB_Q.items():
    for q, h in qs:
        add("kb", q, h, kb=kb)
        if random.random() < 0.35:
            add("kb", q.lower().rstrip("?") + " пж", h if h != "en" else "mix", kb=kb, note="lowercase + пж", lang=None)

KB_PREG = [
    ("Аяғым қақсап жатыр, жүктімін", "preg_legs", "kk"), ("Отекают ноги на 30 неделе беременности", "preg_legs", "ru"),
    ("беременна 28 нед, ноги отекают", "preg_legs", "mix"), ("жүктімін, аяғым ісіп жүр", "preg_legs", "kk"),
    ("Опасен ли гастрит при беременности?", "preg_gastritis", "ru"), ("изжога при беременности что делать", "preg_gastritis", "ru"),
    ("Ребенок мало шевелится, я на 30 неделе", "preg_movements", "ru"), ("жүктімін, баланың қимылы азайды", "preg_movements", "kk"),
    ("Можно ли пить таблетки при беременности?", "preg_medicines", "ru"), ("жүктімін, дәрі ішуге бола ма", "preg_medicines", "kk"),
    ("Токсикоз, тошнит по утрам, я беременна", "preg_nausea", "ru"), ("жүктімін, жүрегім айниды", "preg_nausea", "kk"),
    ("Болит поясница, я беременна", "preg_back", "ru"), ("жүктімін, белім ауырады", "preg_back", "kk"),
    ("Жүктілік кезінде басым ауырады", "preg_headache", "kk"), ("беременна, болит голова", "preg_headache", "ru"),
    ("Какие выделения при беременности нормальные?", "preg_discharge", "ru"),
    ("Простуда при беременности, что делать?", "preg_cold", "ru"), ("Какие витамины пить при беременности?", "preg_nutrition", "ru"),
    ("Когда делают скрининг при беременности?", "preg_screenings", "ru"), ("I'm pregnant, what vitamins should I take?", "preg_nutrition", "en"),
    ("I'm pregnant and my back hurts", "preg_back", "en"), ("pregnant, leg cramps at night", "preg_legs", "en"),
]
for q, kb, h in KB_PREG:
    add("kb_preg", q, h, preg=True, weeks=None if not any(c.isdigit() for c in q) else (30 if "30" in q else 28), kb=kb)

# preg_* must not be answered without pregnancy (chat filters them); complaints need < 2 hits
KB_NONE = [
    ("Болит горло третий день", "ru"), ("Хочу к кардиологу", "ru"), ("Сколько стоит консультация ЛОРа?", "ru"),
    ("Температура и кашель третий день", "ru"), ("Тошнота и рвота после еды", "ru"), ("Болит голова и давление", "ru"),
    ("Болят ноги к вечеру", "ru"), ("Гастрит и изжога", "ru"), ("Можно ли пить эти таблетки?", "ru"), ("Аяғым ауырады", "kk"),
    ("болит спина и поясница", "ru"), ("у ребенка температура 38", "ru"), ("ребёнок кашляет третий день, нужен детский врач", "ru"),
    ("у ребёнка сыпь", "ru"), ("балам ауырып жатыр, қызуы бар", "kk"), ("балам жөтеліп жүр", "kk"), ("my child has a fever", "en"),
    ("тамағым болит уже 3 күн", "mix"), ("басым айналып тұр, давление высокое", "mix"), ("ішім ауырады", "kk"),
    ("сыпь на руке, хочу к дерматологу", "ru"), ("болит ухо, ЛОРға жазылғым келеді", "mix"), ("гинекологқа керек", "mix"),
    ("I have a sore throat", "en"), ("my back hurts", "en"), ("нужен невролог, голова болит", "ru"), ("белім ауырады", "kk"),
    ("тошнит второй день", "ru"), ("выделения и зуд", "ru"), ("отекают ноги", "ru"), ("головная боль и тошнота", "ru"),
    ("кашель у ребёнка, детский сад", "ru"), ("детский лор нужен ребенку", "ru"),
]
for q, h in KB_NONE:
    add("kb_none", q, h, kb="")

# ---------------------------------------------------------------- 10. language detection
LANG = [
    ("У меня болит горло", "ru", ""), ("Тамағым ауырады", "kk", "kk"), ("I have a sore throat", "en", "en"),
    ("my throat hurts, ЛОР?", "en", "en"), ("хочу к ENT врачу", "ru", ""), ("кеудем болит", "mix", "kk", True),
    ("басым ауырады", "kk", "kk"), ("ауырып жатыр", "kk", "kk"), ("керек маған дәрігер", "kk", "kk"), ("рахмет", "kk", "kk"),
    ("калай жазылуга болады", "kk", "kk"), ("OK спасибо", "ru", ""), ("МРТ сколько стоит", "ru", ""), ("hello", "en", "en"),
    ("kudem auyrady", "translit", "kk", True), ("bolit grud", "translit", "", True), ("salem, kerek dariger", "translit", "kk"),
    ("zdravstvuyte, mne nuzhen vrach", "translit", ""), ("pozhaluysta pomogite", "translit", ""), ("Hi, I need a doctor", "en", "en"),
    ("тамақ ауырады керек дәрі", "kk", "kk"), ("жатырмын", "kk", "kk"), ("спасибо большое", "ru", ""),
]
for t, h, want, *red in LANG:
    add("lang", t, h, red=bool(red), lang=want, preg=None)

# ---------------------------------------------------------------- 11. handwritten tricky cases
HAND = [
    # (text, cat, hint, red, preg, weeks, kb, lang, note)
    ("кеудем болит уже 2 часа, отдаёт в руку", "red_mix", "mix", True, False, None, None, "auto", "mixed + duration is not history"),
    ("ГРУДЬ БОЛИТ ОТДАЕТ В ЛЕВУЮ РУКУ СРОЧНО", "red_noisy", "ru", True, False, None, None, "", "caps no punct"),
    ("бабушке плохо она без сознания", "red_ru", "ru", True, False, None, None, "", ""),
    ("у мамы онемела рука и перекосило лицо", "red_ru", "ru", True, False, None, None, "", "stroke"),
    ("муж не может говорить и рука не поднимается", "red_ru", "ru", True, False, None, None, "", "3rd person stroke"),
    ("ребенок посинел и не дышит", "red_ru", "ru", True, False, None, None, "", ""),
    ("отец задыхается, губы синие", "red_ru", "ru", True, False, None, None, "", ""),
    ("кровь хлещет из пореза не останавливается", "red_ru", "ru", True, False, None, None, "", ""),
    ("не хочется жить", "red_ru", "ru", True, False, None, None, "", "suicidal wording"),
    ("жить не хочется совсем", "red_ru", "ru", True, False, None, None, "", ""),
    ("боль в груди не прошла", "red_ru", "ru", True, False, None, None, "", "must stay red"),
    ("раньше такого не было сейчас болит грудь", "red_ru", "ru", True, False, None, None, "", "must stay red"),
    ("мама упала, сознания нет", "red_ru", "ru", True, False, None, None, "", "'нет' belongs to the symptom"),
    ("дышать не могу", "red_ru", "ru", True, False, None, None, "", "word order"),
    ("сердце колет и отдаёт в лопатку", "red_ru", "ru", True, False, None, None, "", ""),
    ("жүрегім қысып тұр", "red_kk", "kk", True, False, None, None, "kk", ""),
    ("анам есі жоқ жатыр", "red_kk", "kk", True, False, None, None, "kk", "'жоқ' belongs to the symptom"),
    ("әкемнің есі жоқ", "red_kk", "kk", True, False, None, None, "kk", "'жоқ' at the end but is the symptom"),
    ("бала тұншығып жатыр", "red_kk", "kk", True, False, None, None, "kk", ""),
    ("тынысым тарылды, бұрын болмаған", "red_kk", "kk", True, False, None, None, "kk", ""),
    ("кеудем ауырғанына 2 сағат болды", "red_kk", "kk", True, False, None, None, "kk", "duration, not history"),
    ("my chest hurts and my left arm is numb", "red_en", "en", True, False, None, None, "en", ""),
    ("I can not breathe", "red_en", "en", True, False, None, None, "en", ""),
    ("my mom passed out", "red_en", "en", True, False, None, None, "en", ""),
    ("she is not breathing", "red_en", "en", True, False, None, None, "en", ""),
    ("темп 39 беременна 30 нед", "red_preg_mix", "mix", True, True, 30, None, None, "abbrev temperature"),
    ("жүктімін, темп 38.5", "red_preg_mix", "mix", True, True, None, None, "kk", "abbrev temperature"),
    ("беременна, температура под 39", "red_preg", "ru", True, True, None, None, "", ""),
    ("32 аптадамын, баланың қимылы жоқ", "red_preg", "kk", True, True, 32, None, "kk", "'жоқ' is the symptom"),
    ("жүктімін, бала қимылдамайды", "red_preg", "kk", True, True, None, None, "kk", ""),
    ("беременна, отёки лица и мушки", "red_preg", "ru", True, True, None, None, "", ""),
    ("I'm pregnant and bleeding", "red_preg", "en", True, True, None, None, "en", ""),
    ("pregnant 34 weeks, my water broke", "red_preg", "en", True, True, 34, None, "en", ""),
    ("жүктімін, көз алдым бұлдырап тұр", "red_preg", "kk", True, True, None, None, "kk", "blurred vision kk"),
    ("беременна, в глазах темнеет, голова сильно болит", "red_preg", "ru", True, True, None, None, "", ""),
    ("жүктімін, бала жақсы қимылдайды", "not_red", "kk", False, True, None, None, "kk", "baby moves = fine"),
    ("я беременна, болит горло", "not_red", "ru", False, True, None, None, "", "pregnant but benign"),
    ("я беременна, отекают ноги", "not_red", "ru", False, True, None, None, "", "yellow, not red"),
    ("жүктімін, жүрегім айниды", "not_red", "kk", False, True, None, None, "kk", "nausea, not red"),
    ("хочу забеременеть, болит низ живота", "not_red", "ru", False, False, None, None, "", "planning != pregnant"),
    ("не могу забеременеть, мажет перед месячными", "not_red", "ru", False, False, None, None, "", "infertility"),
    ("тест на беременность отрицательный, кровь из носа", "not_red", "ru", False, False, None, None, "", ""),
    ("у папы был инсульт, теперь у меня голова кружится", "not_red", "ru", False, False, None, None, "", "relative"),
    ("грудному ребенку 3 месяца, кашель", "not_red", "ru", False, False, None, None, "", ""),
    ("болит грудь и молочные железы перед месячными", "not_red", "ru", False, False, None, None, "", "mastalgia (debatable)"),
    ("мастит, болит грудь", "not_red", "ru", False, False, None, None, "", ""),
    ("кеудемде түйін бар", "not_red", "kk", False, False, None, None, "kk", "lump, mammologist"),
    ("жүрек айну, құсу", "not_red", "kk", False, False, None, None, "kk", "nausea"),
    ("инфаркттан кейін 1 жыл өтті, тексерілгім келеді", "not_red", "kk", False, False, None, None, "kk", ""),
    ("I was unconscious once as a child", "not_red", "en", False, False, None, None, "en", ""),
    ("no seizures, just a headache", "not_red", "en", False, False, None, None, "en", ""),
    ("кровь сдать натощак нужно?", "not_red", "ru", False, False, None, "prep_cbc", "", "'кровь' is not bleeding"),
    ("анализ крови на беременность", "not_red", "ru", False, None, None, None, "", ""),
    ("ЭКГ после инфаркта сколько стоит", "not_red", "ru", False, False, None, None, "", ""),
    ("сколько стоит узи при беременности 12 нед", "preg_detect", "ru", False, True, 12, None, "", "price question"),
    ("срок 8 недель, когда вставать на учет", "kb_preg", "ru", False, True, 8, "preg_screenings", "", ""),
    ("I'm 20 weeks pregnant, when is the next screening?", "kb_preg", "en", False, True, 20, "preg_screenings", "en", ""),
    ("жүктімін, қандай дәрумен ішу керек", "kb_preg", "kk", False, True, None, "preg_nutrition", "kk", ""),
    ("где находится клиника, я беременна", "kb_preg", "ru", False, True, None, "address", "", "general KB wins"),
    ("беременна, болит горло и насморк", "kb_preg", "ru", False, True, None, "preg_cold", "", "cold in pregnancy"),
    ("адрес скиньте", "kb", "ru", False, False, None, "address", "", ""),
    ("до скольки вы сегодня", "kb", "ru", False, False, None, "hours", "", "no keyword (known miss)"),
    ("сколько стоит УЗИ?", "kb_none", "ru", False, False, None, "", "", "prices never from KB"),
    ("у ребёнка температура, детский врач есть?", "kb_none", "ru", False, False, None, "", "", "children duplicates"),
    ("ребенок болеет", "kb_none", "ru", False, False, None, "", "", "children duplicates"),
    ("автобус до вас какой", "kb", "ru", False, False, None, "address", "", ""),
    ("мрт болит голова", "kb_none", "ru", False, False, None, None, "", ""),
    ("kudem auyrady srochno", "red_translit", "translit", True, False, None, None, None, ""),
    ("ne mogu dyshat pomogite", "red_translit", "translit", True, False, None, None, None, ""),
    ("grudnoi rebenok kashlyaet", "not_red", "translit", False, False, None, None, None, "translit trap"),
    # probes for the broader rules added with this suite (must stay green)
    ("не могу дышать носом, насморк неделю", "not_red", "ru", False, False, None, None, "", "nasal congestion"),
    ("нос не дышит третий день", "not_red", "ru", False, False, None, None, "", "nasal congestion"),
    ("носом дышать трудно", "not_red", "ru", False, False, None, None, "", "nasal congestion"),
    ("была рвота, хочу сдать анализ крови", "not_red", "ru", False, False, None, None, "", "'рвот' + 'кров' in one part"),
    ("наглотался воды в бассейне, кашляю", "not_red", "ru", False, False, None, None, "", ""),
    ("припадаю на левую ногу после травмы", "not_red", "ru", False, False, None, None, "", "limping, not a seizure"),
    ("жүрек ауруы бар, кардиологқа жазылғым келеді", "not_red", "kk", False, False, None, None, "kk", "known heart disease, booking"),
    ("сердце проверить хочу", "not_red", "ru", False, False, None, None, "", ""),
    ("губы сохнут и трескаются", "not_red", "ru", False, False, None, None, "", ""),
    ("sudorogi v nogah po nocham", "not_red", "translit", False, False, None, None, None, ""),
    ("u papy byl infarkt", "not_red", "translit", False, False, None, None, None, ""),
    ("мы берем 2 новых талона", "preg_negative", "ru", False, False, None, None, "", "'берем' + digit is not pregnancy"),
    ("that's an insult to my intelligence, where is the clinic", "not_red", "en", False, False, None, None, "en", "'insult' in English"),
]
for text, cat, hint, red, preg, weeks, kb, lang, note in HAND:
    add(cat, text, hint, red=red, preg=preg, weeks=weeks, kb=kb, lang=lang, note=note, src="hand")

with open(OUT, "w", encoding="utf-8") as f:
    for c in cases:
        f.write(json.dumps(c, ensure_ascii=False) + "\n")
print(len(cases), "cases ->", OUT)
