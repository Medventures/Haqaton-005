import type { Appointment, AppointmentCreate, AppointmentResponse, Catalog, ChatResponse, DialogResponse, Message, Ticket, UploadResponse } from './types'
import { ApiError } from './errors'
const dialogs = new Map<string, DialogResponse>()
const services = [{ id: 'therapy', specialty_id: 'therapy', name: 'Приём терапевта', price: 8000, description: 'Первичная консультация специалиста.' }]
const doctors = [{ id: 'doctor-1', specialty_id: 'therapy', name: 'Айдана Сәрсенова', slots: ['2026-10-01T09:00', '2026-10-01T11:30'] }]
const files = new Map<string, Blob>()
const appointments: Appointment[] = []
// shown for attachment ids the mock has never seen (e.g. after a reload)
const placeholder = () => new Blob(['<svg xmlns="http://www.w3.org/2000/svg" width="440" height="300"><rect width="440" height="300" fill="#EEF1EE"/><text x="220" y="156" font-family="Inter, sans-serif" font-size="20" fill="#5D6963" text-anchor="middle">Файл недоступен в демо-режиме</text></svg>'], { type: 'image/svg+xml' })
const wait = (ms = 700) => new Promise(resolve => setTimeout(resolve, ms))
export const mock = {
  async patient() { await wait(150); return { token: 'mock-patient-token', user_id: 'demo-patient', role: 'patient' as const } },
  async operatorLogin() { return { token: 'mock-operator-token', role: 'operator' as const } },
  async chat(message: string, id?: string): Promise<ChatResponse> {
    await wait()
    const dialog_id = id || crypto.randomUUID(), text = message.toLowerCase()
    const urgency = /кеуде|ентігу|груд|дыхани/.test(text) ? 'red' : /қызу|температур|басым|голов/.test(text) ? 'yellow' : 'green'
    const reply = urgency === 'red' ? 'Срочно позвоните 103. Не ждите ответа в чате.' : urgency === 'yellow' ? 'Рекомендуем обратиться за медицинской оценкой в ближайшее время. Сколько дней сохраняются симптомы?' : /қазақ|сәлем|ауырып|ауыр/.test(text) ? 'Түсіндім. Белгілеріңіз қашан басталды? Тағы не мазалайды?' : 'Понял вас. Подскажите, как давно это началось?'
    const actions = urgency === 'red' ? ['call_103', 'contact_operator'] : urgency === 'yellow' ? ['contact_operator'] : []
    const botMessage = { role: 'bot' as const, content: reply, data: { urgency: urgency as ChatResponse['urgency'], actions, services: urgency === 'green' ? services : [], doctors: urgency === 'green' ? doctors : [] } }
    const prev = dialogs.get(dialog_id) || { dialog: { id: dialog_id, status: 'bot' as const, language: 'ru' as const, urgency: 'green' as const }, messages: [] }
    const next: DialogResponse = { ...prev, dialog: { ...prev.dialog, urgency: urgency as ChatResponse['urgency'], summary: message }, messages: [...prev.messages, { role: 'patient', content: message }, botMessage] }
    dialogs.set(dialog_id, next)
    return { dialog_id, status: 'bot', urgency: urgency as ChatResponse['urgency'], language: /[әғқңөұүһі]/i.test(message) ? 'kk' : 'ru', reply: botMessage, actions, services: urgency === 'green' ? services : [], doctors: urgency === 'green' ? doctors : [] }
  },
  async dialog(id: string) { await wait(100); return dialogs.get(id) || { dialog: { id, status: 'bot' as const, language: 'ru' as const, urgency: 'green' as const }, messages: [] } },
  async handoff(id: string) { const d = dialogs.get(id); if (d) d.dialog.status = 'operator'; return { ticket_id: 1, status: 'open', reply: { role: 'bot' as const, content: 'Обращение передано оператору.' } } },
  async queue(): Promise<Ticket[]> { return [...dialogs.values()].filter(d => d.dialog.status === 'operator').map((d, i) => ({ id: i + 1, dialog_id: d.dialog.id, reason: 'Запрошен оператор', summary: d.dialog.summary || '', status: 'open', urgency: d.dialog.urgency, last_message: d.messages.at(-1)?.content })) },
  async upload(file: File, id?: string): Promise<UploadResponse> {
    await wait()
    const dialog_id = id || crypto.randomUUID(), attachmentId = crypto.randomUUID()
    files.set(attachmentId, file)
    const message: Message = { role: 'patient', content: file.name, data: { attachments: [{ id: attachmentId, name: file.name, content_type: file.type, size: file.size }] }, created_at: new Date().toISOString() }
    const reply: Message = { role: 'bot', content: 'Файл получен. Врач или оператор посмотрит его вместе с вашим обращением.', created_at: new Date().toISOString() }
    const prev = dialogs.get(dialog_id) || { dialog: { id: dialog_id, status: 'bot' as const, language: 'ru' as const, urgency: 'green' as const }, messages: [] }
    dialogs.set(dialog_id, { ...prev, messages: [...prev.messages, message, reply] })
    return { dialog_id, status: prev.dialog.status, message, reply }
  },
  async attachmentBlob(id: string) { await wait(150); return files.get(id) || placeholder() },
  async reply(id: string, message?: string, close = false) { const d = dialogs.get(id); if (d && message) d.messages.push({ role: 'operator', content: message }); if (d && close) d.dialog.status = 'bot'; return { ok: true } },
  async catalog(): Promise<Catalog> { await wait(150); return { services, doctors: doctors.map(doctor => ({ ...doctor, slots: [...doctor.slots] })) } },
  // fake booking: same rules the server enforces that the UI reacts to (400 phone, 409 taken slot)
  async book(body: AppointmentCreate): Promise<AppointmentResponse> {
    await wait(500)
    if (body.phone.replace(/\D/g, '').length !== 11) throw new ApiError('Телефон должен быть казахстанским: +7 и 10 цифр', 400, 'Телефон должен быть казахстанским: +7 и 10 цифр')
    const doctor = doctors.find(item => item.id === body.doctor_id)
    if (!doctor?.slots.includes(body.slot)) throw new ApiError('Слот уже занят', 409, 'Слот уже занят')
    doctor.slots = doctor.slots.filter(slot => slot !== body.slot)
    const now = new Date().toISOString()
    const appointment: Appointment = { id: crypto.randomUUID(), dialog_id: body.dialog_id ?? null, patient_id: 'demo-patient', doctor_id: body.doctor_id, service_id: body.service_id, slot: body.slot, patient_name: body.patient_name, phone: body.phone, status: 'booked', created_at: now, updated_at: now }
    appointments.unshift(appointment)
    const service = services.find(item => item.id === body.service_id)
    const reply: Message = { role: 'bot', content: `Вы записаны: ${service?.name ?? body.service_id}, ${doctor.name}, ${body.slot.replace('T', ' ')}.`, created_at: now }
    return { appointment, reply }
  },
  async appointments() { await wait(150); return appointments.map(item => ({ ...item })) },
  async cancelAppointment(id: string) {
    await wait(300)
    const appointment = appointments.find(item => item.id === id)
    if (!appointment) throw new ApiError('Запись не найдена', 404, 'Запись не найдена')
    if (appointment.status === 'cancelled') throw new ApiError('Запись уже отменена', 409, 'Запись уже отменена')
    appointment.status = 'cancelled'; appointment.updated_at = new Date().toISOString()
    const doctor = doctors.find(item => item.id === appointment.doctor_id)
    if (doctor && !doctor.slots.includes(appointment.slot)) doctor.slots = [...doctor.slots, appointment.slot].sort()
    return { ...appointment }
  },
}
