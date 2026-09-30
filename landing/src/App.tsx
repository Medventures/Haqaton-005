import { useEffect, useState } from 'react'
import type { Lang } from './i18n'
import { dict } from './i18n'
import { Demo } from './Demo'
import { Api, Levels, Operators, Pipeline } from './Sections'

const CHAT_URL = import.meta.env.VITE_CHAT_URL || 'http://localhost:5180/'
const API_URL = import.meta.env.VITE_API_URL || 'http://localhost:8088'
const langs: { id: Lang; label: string }[] = [
  { id: 'kk', label: 'Қаз' },
  { id: 'ru', label: 'Рус' },
  { id: 'en', label: 'Eng' },
]

function initialLang(): Lang {
  try {
    const saved = localStorage.getItem('lang')
    if (saved === 'ru' || saved === 'kk' || saved === 'en') return saved
  } catch {
    /* storage unavailable */
  }
  return navigator.language.slice(0, 2) === 'kk' ? 'kk' : 'ru'
}

export default function App() {
  const [lang, setLang] = useState<Lang>(initialLang)
  const t = dict[lang]

  useEffect(() => {
    document.documentElement.lang = lang
    try {
      localStorage.setItem('lang', lang)
    } catch {
      /* ignore */
    }
  }, [lang])

  return (
    <>
      <header className="top">
        <a className="brand" href="#">
          <span className="brand-dots" aria-hidden>
            <i className="dot dot-green" />
            <i className="dot dot-yellow" />
            <i className="dot dot-red" />
          </span>
          MedHub Clinic
        </a>
        <nav className="nav">
          <a href="#how">{t.nav.how}</a>
          <a href="#levels">{t.nav.levels}</a>
          <a href="#operators">{t.nav.operators}</a>
          <a href="#api">{t.nav.api}</a>
        </nav>
        <div className="top-right">
          <div className="langs" role="group" aria-label="Language">
            {langs.map((l) => (
              <button key={l.id} className={lang === l.id ? 'on' : ''} aria-pressed={lang === l.id} onClick={() => setLang(l.id)}>
                {l.label}
              </button>
            ))}
          </div>
          <a className="btn btn-primary btn-sm" href={CHAT_URL}>
            {t.nav.open}
          </a>
        </div>
      </header>

      <main>
        <section className="hero">
          <div className="hero-text">
            <h1>{t.hero.title}</h1>
            <p className="hero-lead">{t.hero.lead}</p>
            <div className="hero-cta">
              <a className="btn btn-primary" href={CHAT_URL}>
                {t.hero.open}
              </a>
              <a className="btn btn-quiet" href={`${CHAT_URL.replace(/\/$/, '')}/operator`}>
                {t.hero.operator}
              </a>
            </div>
          </div>
          <Demo lang={lang} />
        </section>

        <Pipeline lang={lang} />
        <Levels lang={lang} />
        <Operators lang={lang} />
        <Api lang={lang} apiUrl={API_URL} />
      </main>

      <footer className="foot">
        <p className="safety">{t.footer.safety}</p>
        <p className="stack">{t.footer.stack}</p>
      </footer>
    </>
  )
}
