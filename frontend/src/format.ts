import type { Language, Service } from './api/types'

export function locale(language: Language | string) { return language === 'kk' ? 'kk-KZ' : language === 'en' ? 'en-US' : 'ru-RU' }

const KK_MONTHS = ['қаң', 'ақп', 'нау', 'сәу', 'мам', 'мау', 'шіл', 'там', 'қыр', 'қаз', 'қар', 'жел']
const KK_DAYS = ['жек', 'дүй', 'сей', 'сәр', 'бей', 'жұм', 'сен']
// slots are local clinic time without a zone ("2026-10-01T09:00"): new Date() reads them as local time
export function formatSlot(value: string, language: Language | string) {
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value
  // browsers without kk ICU data silently fall back to English: format Kazakh by hand («1 қаз, сәр · 09:00», DESIGN.md)
  if (language === 'kk') {
    const time = `${String(date.getHours()).padStart(2, '0')}:${String(date.getMinutes()).padStart(2, '0')}`
    return `${date.getDate()} ${KK_MONTHS[date.getMonth()]}, ${KK_DAYS[date.getDay()]} · ${time}`
  }
  return new Intl.DateTimeFormat(locale(language), { weekday: 'short', day: 'numeric', month: 'short', hour: '2-digit', minute: '2-digit' }).format(date)
}

export function serviceName(service: Service, language: Language) { return service.i18n?.[language]?.name ?? service.name }

// «+7 701 123-45-67» while typing. The field starts with «+7», so the patient types the 10 national digits;
// a domestic «8 …» or a repeated country code «7 …» typed after the prefix is dropped
// (Kazakhstan numbers never start with 8 after +7).
export function formatPhone(raw: string, previous = '') {
  const digits = raw.replace(/\D/g, '')
  if (!digits) return ''
  // one more key after a complete number: ignore it instead of shifting the digits
  if (phoneComplete(previous) && digits.length === 12 && raw.length === previous.length + 1) return previous
  let rest = raw.trim().startsWith('+') || (digits.length >= 11 && /^[78]/.test(digits)) ? digits.slice(1) : digits
  if (rest.length > 10 && /^[78]/.test(rest)) rest = rest.slice(1)
  else if (rest.startsWith('8')) rest = rest.slice(1)
  rest = rest.slice(0, 10)
  let out = '+7'
  if (rest.length) out += ' ' + rest.slice(0, 3)
  if (rest.length > 3) out += ' ' + rest.slice(3, 6)
  if (rest.length > 6) out += '-' + rest.slice(6, 8)
  if (rest.length > 8) out += '-' + rest.slice(8, 10)
  return out
}
export const phoneComplete = (phone: string) => phone.replace(/\D/g, '').length === 11
