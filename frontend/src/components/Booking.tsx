import { useEffect, useState, type FormEvent } from 'react'
import { Check } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { api, ApiError } from '../api/client'
import type { Appointment, Doctor, Language, Message, Service } from '../api/types'
import { formatPhone, formatSlot, locale, phoneComplete, serviceName } from '../format'

const CONTACT_KEY = 'booking_contact'
function savedContact(): { name: string; phone: string } {
  try { const value = JSON.parse(localStorage.getItem(CONTACT_KEY) || '{}'); return { name: String(value.name || ''), phone: String(value.phone || '') } }
  catch { return { name: '', phone: '' } }
}
function saveContact(name: string, phone: string) {
  try { localStorage.setItem(CONTACT_KEY, JSON.stringify({ name, phone })) } catch { /* private mode: nothing to reuse next time */ }
}

type Booking = { appointment: Appointment; doctor: string; cancelled?: boolean; cancelling?: boolean; error?: string }
type Preset = { doctorId: string; slot: string; nonce: number }
const slotKey = (doctorId: string, slot: string) => `${doctorId}|${slot}`
const doctorsFor = (service: Service, doctors: Doctor[]) => service.specialty_id ? doctors.filter(doctor => !doctor.specialty_id || doctor.specialty_id === service.specialty_id) : doctors

// Service cards + «Врачи и свободное время» of one bot message, with an inline booking panel under a card.
export function ServiceOffers({ services, doctors, language, dialogId, onReply }: { services: Service[]; doctors: Doctor[]; language: Language; dialogId?: string; onReply: (message: Message) => void }) {
  const { t } = useTranslation()
  const [open, setOpen] = useState('')
  const [preset, setPreset] = useState<Preset | null>(null)
  // slots gone since the message arrived (booked here, or taken by someone else: 409)
  const [gone, setGone] = useState<Set<string>>(() => new Set())
  // fresh free slots from /api/catalog after a 409, by doctor id
  const [fresh, setFresh] = useState<Record<string, string[]>>({})
  const [bookings, setBookings] = useState<Record<string, Booking>>({})
  const slotsOf = (doctor: Doctor) => (fresh[doctor.id] ?? doctor.slots).filter(slot => !gone.has(slotKey(doctor.id, slot)))
  const markGone = (doctorId: string, slot: string, isGone: boolean) => setGone(current => { const next = new Set(current); if (isGone) next.add(slotKey(doctorId, slot)); else next.delete(slotKey(doctorId, slot)); return next })

  async function refreshSlots() {
    try { const catalog = await api.catalog(); setFresh(Object.fromEntries(catalog.doctors.map(doctor => [doctor.id, doctor.slots]))) } catch { /* the local removal is enough */ }
  }
  function pickSlot(doctor: Doctor, slot: string) {
    const fits = (item: Service) => doctorsFor(item, [doctor]).length > 0 && (!bookings[item.id] || Boolean(bookings[item.id].cancelled))
    const current = services.find(item => item.id === open)
    const service = current && fits(current) ? current : services.find(fits)
    if (!service) return
    setOpen(service.id); setPreset({ doctorId: doctor.id, slot, nonce: Date.now() })
  }
  async function cancel(serviceId: string) {
    const booking = bookings[serviceId]
    if (!booking || booking.cancelling) return
    setBookings(current => ({ ...current, [serviceId]: { ...booking, cancelling: true, error: '' } }))
    try {
      await api.cancelAppointment(booking.appointment.id)
      markGone(booking.appointment.doctor_id, booking.appointment.slot, false)
      setBookings(current => ({ ...current, [serviceId]: { ...booking, cancelled: true, cancelling: false } }))
    } catch (e) {
      setBookings(current => ({ ...current, [serviceId]: { ...booking, cancelling: false, error: e instanceof ApiError && e.serverText ? e.serverText : t('booking.cancelError') } }))
    }
  }

  return <>
    {services.length > 0 && <div className="services-list">{services.map(service => {
      const specialty = typeof service.specialty === 'string' ? service.specialty : service.specialty?.i18n?.[language]?.name ?? service.specialty?.name
      const booking = bookings[service.id]
      const booked = booking && !booking.cancelled
      const isOpen = open === service.id && !booked
      return <article className={`service-card${isOpen ? ' is-booking' : ''}`} key={service.id}>
        <h3>{serviceName(service, language)}</h3>
        {(service.i18n?.[language]?.specialty ?? specialty) && <span className="service-specialty">{service.i18n?.[language]?.specialty ?? specialty}</span>}
        <p>{service.i18n?.[language]?.description ?? service.description}</p>
        <div className="service-bottom"><strong>{service.price.toLocaleString(locale(language))} ₸</strong>{!booked && <button type="button" aria-expanded={isOpen} onClick={() => { setOpen(isOpen ? '' : service.id); setPreset(null) }}>{t('book')}</button>}</div>
        {booking && (booking.cancelled
          ? <p className="booking-result is-cancelled" role="status">{t('booking.cancelled')}</p>
          : <div className="booking-result" role="status"><Check size={18} strokeWidth={1.75} aria-hidden="true" /><span>{t('booking.done', { service: serviceName(service, language), doctor: booking.doctor, slot: formatSlot(booking.appointment.slot, language).replace(/ /g, '\u00A0') })}</span><button type="button" className="link-button" disabled={booking.cancelling} onClick={() => void cancel(service.id)}>{booking.cancelling ? t('booking.cancelling') : t('booking.cancel')}</button>{booking.error && <span className="booking-note">{booking.error}</span>}</div>)}
        {isOpen && <BookingPanel service={service} doctors={doctorsFor(service, doctors)} slotsOf={slotsOf} preset={preset} language={language} dialogId={dialogId}
          onClose={() => setOpen('')}
          onTaken={(doctorId, slot) => { markGone(doctorId, slot, true); void refreshSlots() }}
          onBooked={(appointment, doctor, reply) => { markGone(appointment.doctor_id, appointment.slot, true); setBookings(current => ({ ...current, [service.id]: { appointment, doctor } })); setOpen(''); if (reply) onReply(reply) }} />}
      </article>
    })}</div>}
    {doctors.length > 0 && <section className="doctors-block"><h4>{t('doctors')}</h4>{doctors.map(doctor => {
      const bookable = services.some(service => doctorsFor(service, [doctor]).length)
      const slots = slotsOf(doctor)
      return <div className="doctor-row" key={doctor.id}><b>{doctor.name}</b><div className="slots">{slots.map(slot => bookable
        ? <button type="button" key={slot} className="slot-chip" onClick={() => pickSlot(doctor, slot)}>{formatSlot(slot, language)}</button>
        : <span key={slot}>{formatSlot(slot, language)}</span>)}{!slots.length && <span className="slots-empty">{t('booking.noSlots')}</span>}</div></div>
    })}</section>}
  </>
}

function BookingPanel({ service, doctors, slotsOf, preset, language, dialogId, onClose, onTaken, onBooked }: {
  service: Service; doctors: Doctor[]; slotsOf: (doctor: Doctor) => string[]; preset: Preset | null; language: Language; dialogId?: string
  onClose: () => void; onTaken: (doctorId: string, slot: string) => void; onBooked: (appointment: Appointment, doctorName: string, reply?: Message | null) => void
}) {
  const { t } = useTranslation()
  const firstFree = doctors.find(doctor => slotsOf(doctor).length) ?? doctors[0]
  const [doctorId, setDoctorId] = useState(preset?.doctorId ?? firstFree?.id ?? '')
  const [slot, setSlot] = useState(preset?.slot ?? '')
  const [name, setName] = useState(() => savedContact().name)
  const [phone, setPhone] = useState(() => savedContact().phone)
  const [sending, setSending] = useState(false)
  const [note, setNote] = useState('')
  const doctor = doctors.find(item => item.id === doctorId)
  const slots = doctor ? slotsOf(doctor) : []

  // a slot clicked in the doctor list while the panel is open moves the selection there
  useEffect(() => { if (preset) { setDoctorId(preset.doctorId); setSlot(preset.slot); setNote('') } }, [preset?.nonce])
  // the chosen slot disappeared (409 or refreshed list): make the patient choose again
  useEffect(() => { if (slot && !slots.includes(slot)) setSlot('') }, [slots.join(',')])

  const ready = Boolean(doctor && slot && name.trim().length >= 2 && phoneComplete(phone)) && !sending
  async function submit(event: FormEvent) {
    event.preventDefault()
    if (!ready || !doctor) return
    setSending(true); setNote('')
    const patientName = name.trim()
    saveContact(patientName, phone)
    try {
      const result = await api.book({ doctor_id: doctor.id, service_id: service.id, slot, patient_name: patientName, phone, ...(dialogId ? { dialog_id: dialogId } : {}) })
      onBooked(result.appointment, doctor.name, result.reply)
    } catch (e) {
      if (e instanceof ApiError && e.status === 409) { setNote(t('booking.taken')); onTaken(doctor.id, slot); setSlot('') }
      else if (e instanceof ApiError && e.status === 400 && e.serverText) setNote(e.serverText)
      else setNote(e instanceof ApiError && e.serverText ? e.serverText : t('booking.error'))
    } finally { setSending(false) }
  }

  const id = `booking-${service.id}`
  return <form className="booking-panel" onSubmit={submit} aria-label={t('booking.title', { service: serviceName(service, language) })}>
    {!doctors.length ? <p className="booking-note">{t('booking.noDoctors')}</p> : <>
      <fieldset className="booking-field"><legend>{t('booking.doctor')}</legend><div className="booking-options">{doctors.map(item => <button type="button" key={item.id} className="booking-option" aria-pressed={item.id === doctorId} onClick={() => { setDoctorId(item.id); setNote('') }}>{item.name}</button>)}</div></fieldset>
      <fieldset className="booking-field"><legend>{t('booking.slot')}</legend>{slots.length ? <div className="slots booking-slots">{slots.map(value => <button type="button" key={value} className="slot-chip" aria-pressed={value === slot} onClick={() => { setSlot(value); setNote('') }}>{formatSlot(value, language)}</button>)}</div> : <p className="booking-hint">{t('booking.noSlots')}</p>}</fieldset>
      <div className="booking-contacts">
        <label htmlFor={`${id}-name`}>{t('booking.name')}<input id={`${id}-name`} value={name} onChange={e => setName(e.target.value)} autoComplete="name" maxLength={100} required /></label>
        <label htmlFor={`${id}-phone`}>{t('booking.phone')}<input id={`${id}-phone`} type="tel" inputMode="tel" value={phone} onChange={e => setPhone(formatPhone(e.target.value, phone))} onFocus={() => { if (!phone) setPhone('+7') }} placeholder="+7 7__ ___-__-__" autoComplete="tel" required /></label>
      </div>
      {note && <p className="booking-note" role="alert">{note}</p>}
      <div className="booking-actions"><button type="submit" className="booking-submit" disabled={!ready}>{sending ? t('booking.sending') : t('booking.submit')}</button><button type="button" className="booking-close" onClick={onClose} disabled={sending}>{t('booking.hide')}</button></div>
    </>}
  </form>
}
