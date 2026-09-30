import i18n from 'i18next'
import { initReactI18next } from 'react-i18next'
import ru from './locales/ru.json'
import kk from './locales/kk.json'
import en from './locales/en.json'

void i18n.use(initReactI18next).init({
  resources: { ru: { translation: ru }, kk: { translation: kk }, en: { translation: en } },
  lng: 'ru', fallbackLng: 'ru', supportedLngs: ['ru', 'kk', 'en'], interpolation: { escapeValue: false },
})
export default i18n
