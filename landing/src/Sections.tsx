import { useEffect, useRef, useState } from 'react'
import { motion, useInView, useReducedMotion } from 'motion/react'
import type { Lang, Urgency } from './i18n'
import { dict } from './i18n'

const rank: Record<Urgency, number> = { red: 0, yellow: 1, green: 2 }

// Five real steps of the pipeline; the step in the middle of the viewport becomes active.
export function Pipeline({ lang }: { lang: Lang }) {
  const t = dict[lang].how
  const [active, setActive] = useState(0)
  const refs = useRef<(HTMLLIElement | null)[]>([])

  useEffect(() => {
    const io = new IntersectionObserver(
      (entries) => {
        for (const e of entries) {
          if (e.isIntersecting) setActive(Number((e.target as HTMLElement).dataset.i))
        }
      },
      { rootMargin: '-45% 0px -45% 0px' },
    )
    refs.current.forEach((el) => el && io.observe(el))
    return () => io.disconnect()
  }, [])

  return (
    <section className="section how" id="how">
      <div className="how-aside">
        <h2>{t.title}</h2>
        <p className="lead">{t.lead}</p>
        <div className="how-progress" aria-hidden>
          <span style={{ transform: `scaleY(${(active + 1) / t.steps.length})` }} />
        </div>
      </div>
      <ol className="steps">
        {t.steps.map((s, i) => (
          <li
            key={i}
            data-i={i}
            ref={(el) => {
              refs.current[i] = el
            }}
            className={`step ${i === active ? 'active' : ''} ${i < active ? 'done' : ''}`}
          >
            <span className="step-n">{i + 1}</span>
            <div>
              <h3>
                {s.title}
                {i === 0 && <span className="tag">{lang === 'en' ? 'no AI' : lang === 'kk' ? 'ЖИ-сіз' : 'без ИИ'}</span>}
              </h3>
              <p>{s.text}</p>
            </div>
          </li>
        ))}
      </ol>
    </section>
  )
}

export function Levels({ lang }: { lang: Lang }) {
  const t = dict[lang].scale
  const levels: Urgency[] = ['green', 'yellow', 'red']
  return (
    <section className="section levels" id="levels">
      <h2>{t.title}</h2>
      <p className="lead">{t.lead}</p>
      <div className="scale" aria-hidden>
        {levels.map((u) => (
          <span key={u} className={`scale-seg seg-${u}`} />
        ))}
      </div>
      <div className="level-cols">
        {levels.map((u) => (
          <div key={u} className={`level level-${u}`}>
            <h3>
              <span className={`dot dot-${u}`} />
              {t.items[u].name}
            </h3>
            <p>{t.items[u].text}</p>
            <ul className="examples">
              {t.items[u].examples.map((e) => (
                <li key={e}>«{e}»</li>
              ))}
            </ul>
          </div>
        ))}
      </div>
      <p className="note">{t.note}</p>
    </section>
  )
}

// Operator queue: cases arrive in time order, then re-sort red → yellow → green when scrolled into view.
export function Operators({ lang }: { lang: Lang }) {
  const d = dict[lang]
  const t = d.ops
  const ref = useRef<HTMLDivElement>(null)
  const inView = useInView(ref, { once: true, amount: 0.5 })
  const reduce = useReducedMotion()
  const [sorted, setSorted] = useState(false)

  useEffect(() => {
    if (!inView) return
    if (reduce) {
      setSorted(true)
      return
    }
    const id = window.setTimeout(() => setSorted(true), 900)
    return () => clearTimeout(id)
  }, [inView, reduce])

  const items = t.tickets.map((tk, i) => ({ ...tk, id: i }))
  const list = sorted ? [...items].sort((a, b) => rank[a.u] - rank[b.u] || a.id - b.id) : items
  const red = items.find((x) => x.u === 'red')!

  return (
    <section className="section ops" id="operators">
      <div className="ops-text">
        <h2>{t.title}</h2>
        <p className="lead">{t.lead}</p>
      </div>
      <div className="console" ref={ref}>
        <div className="queue">
          <div className="pane-title">{t.queue}</div>
          <ul>
            {list.map((x) => (
              <motion.li key={x.id} layout transition={{ type: 'tween', duration: 0.5, ease: [0.2, 0, 0, 1] }} className={`ticket t-${x.u} ${x.u === 'red' && sorted ? 'selected' : ''}`}>
                <span className={`badge badge-${x.u}`}>{d.levels[x.u]}</span>
                <span className="ticket-text">{x.text}</span>
                <span className="ticket-reason">{x.reason}</span>
              </motion.li>
            ))}
          </ul>
        </div>
        <div className="case">
          <div className="pane-title">{t.summaryTitle}</div>
          <div className="case-tags">
            <span className="badge badge-red">{d.levels.red}</span>
            <span className="badge badge-plain">{t.pregnant}</span>
          </div>
          <dl>
            <dt>{t.want}</dt>
            <dd>{d.demo[1][0].text}</dd>
            <dt>{t.found}</dt>
            <dd>{d.demo[1][2].text}</dd>
            <dt>{t.why}</dt>
            <dd>{red.reason}</dd>
          </dl>
        </div>
      </div>
    </section>
  )
}

export function Api({ lang, apiUrl }: { lang: Lang; apiUrl: string }) {
  const t = dict[lang].api
  return (
    <section className="section api" id="api">
      <div>
        <h2>{t.title}</h2>
        <p className="lead">{t.lead}</p>
        <a className="text-link" href={`${apiUrl}/api/openapi.yaml`}>
          {t.spec}: /api/openapi.yaml
        </a>
      </div>
      <table className="endpoints">
        <tbody>
          {t.rows.map(([m, p, desc]) => (
            <tr key={p}>
              <td className="method">{m}</td>
              <td className="path">{p}</td>
              <td>{desc}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </section>
  )
}
