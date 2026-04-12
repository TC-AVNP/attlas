import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import './index.css'
import App from './App.jsx'
import { initTheme } from './components/ThemeToggle.jsx'

// Apply persisted theme synchronously before React mounts, so there's
// no flash-of-wrong-theme on page load.
initTheme()

createRoot(document.getElementById('root')).render(
  <StrictMode>
    <App />
  </StrictMode>,
)
