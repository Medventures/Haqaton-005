import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import './index.css'
import './patient-layout.css'
import './chat.css'
import './operator.css'
import './operator-conversation.css'
import './booking.css'
import './responsive.css'
import './i18n'
import App from './App.tsx'

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <App />
  </StrictMode>,
)
