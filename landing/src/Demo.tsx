import { useEffect, useState } from 'react'
import { AnimatePresence, motion, useReducedMotion } from 'motion/react'
import { Headset, PhoneCall, RotateCcw } from 'lucide-react'
import type { DemoStep, Lang, Urgency } from './i18n'
import { dict } from './i18n'

const order: Urgency[] = ['green', 'yellow', 'red']
const ease = [0.2, 0, 0, 1] as const

// Plays the scripted conversations one after another: green (service card), then red (pregnancy).
export function Demo({ lang }: { lang: Lang }) {
  const t = dict[lang]
  const reduce = useReducedMotion()
  const [run, setRun] = useState(0)
  const [scene, setScene] = useState(0)
  const [shown, setShown] = useState<DemoStep[]>([])
  const [typing, setTyping] = useState(false)

  useEffect(() => {
    const scenes = t.demo
    if (reduce) {
      setScene(scenes.length - 1)
      setShown(scenes[scenes.length - 1])
      setTyping(false)
      return
    }
    let cancelled = false
    const timers: number[] = []
    const wait = (ms: number) => new Promise<void>((r) => timers.push(window.setTimeout(r, ms)))
    ;(async () => {
      for (let s = 0; s < scenes.length; s++) {
        if (cancelled) return
        setScene(s)
        setShown([])
        await wait(500)
        for (const step of scenes[s]) {
          if (cancelled) return
          if (step.kind === 'bot') {
            setTyping(true)
            await wait(1100)
            setTyping(false)
          } else {
            await wait(700)
          }
          setShown((prev) => [...prev, step])
          await wait(step.kind === 'bot' ? 900 : 300)
        }
        if (s < scenes.length - 1) await wait(2600)
      }
    })()
    return () => {
      cancelled = true
      timers.forEach(clearTimeout)
    }
  }, [lang, run, reduce, t.demo])

  const level = shown.reduce<Urgency | null>((acc, st) => {
    if (st.kind !== 'bot' || !st.urgency) return acc
    if (!acc) return st.urgency
    return order.indexOf(st.urgency) > order.indexOf(acc) ? st.urgency : acc
  }, null)

  return (
    <figure className="demo" aria-label={t.hero.demoLabel}>
      <div className="demo-head">
        <span className="demo-title">{t.hero.demoLabel}</span>
        <button className="icon-btn" onClick={() => setRun((r) => r + 1)} aria-label={t.hero.replay} title={t.hero.replay}>
          <RotateCcw size={16} strokeWidth={1.75} />
        </button>
      </div>

      <div className="meter" role="img" aria-label={level ? t.levels[level] : ''}>
        {order.map((u) => (
          <div key={u} className={`meter-seg seg-${u} ${level === u ? 'on' : ''}`}>
            <span className="meter-fill" />
            <span className="meter-label">{t.levels[u]}</span>
          </div>
        ))}
      </div>

      <div className="stream" key={`${scene}-${run}-${lang}`}>
        <AnimatePresence initial={false}>
          {shown.map((st, i) => (
            <motion.div
              key={i}
              className={`msg ${st.kind}`}
              initial={{ opacity: 0, y: 8 }}
              animate={{ opacity: 1, y: 0 }}
              transition={{ duration: 0.22, ease }}
            >
              <p className="bubble">{st.text}</p>
              {st.kind === 'bot' && st.card && (
                <div className="svc">
                  <div className="svc-row">
                    <span className="svc-name">{st.card.name}</span>
                    <span className="svc-price">{st.card.price}</span>
                  </div>
                  <div className="slots">
                    {st.card.slots.map((s) => (
                      <span key={s} className="slot">{s}</span>
                    ))}
                  </div>
                </div>
              )}
              {st.kind === 'bot' && st.actions && (
                <div className="actions">
                  <a className="act act-103" href="tel:103">
                    <PhoneCall size={18} strokeWidth={1.75} />
                    {st.actions[0]}
                  </a>
                  {st.actions[1] && (
                    <span className="act act-op">
                      <Headset size={18} strokeWidth={1.75} />
                      {st.actions[1]}
                    </span>
                  )}
                </div>
              )}
              {st.kind === 'bot' && (st.urgency === 'red' || st.urgency === 'yellow') && <p className="disclaimer">{t.disclaimer}</p>}
            </motion.div>
          ))}
        </AnimatePresence>
        {typing && (
          <div className="msg bot" aria-label={t.typing}>
            <p className="bubble typing">
              <span />
              <span />
              <span />
            </p>
          </div>
        )}
      </div>
    </figure>
  )
}
