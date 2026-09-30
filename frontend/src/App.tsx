import PatientPage from './pages/PatientPage'
import OperatorPage from './pages/OperatorPage'

export default function App() {
  return window.location.pathname.replace(/\/$/, '') === '/operator' ? <OperatorPage /> : <PatientPage />
}
