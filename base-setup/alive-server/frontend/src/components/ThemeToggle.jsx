import { useEffect, useState } from 'react'

// Persistent dark/light theme toggle.
//
// Contract:
// - Theme is applied by setting `data-theme="dark"|"light"` on <html>.
//   CSS variables under those selectors (see index.css) do the rest.
// - First-visit default honors `prefers-color-scheme`. After the user
//   explicitly toggles, the localStorage value wins and the system
//   preference is ignored.
// - The module-level init in main.jsx sets the attribute synchronously
//   before first paint so there's no flash of the wrong theme on reload.

const STORAGE_KEY = 'attlas-theme'

export function initTheme() {
  const stored = localStorage.getItem(STORAGE_KEY)
  if (stored === 'dark' || stored === 'light') {
    document.documentElement.setAttribute('data-theme', stored)
    return stored
  }
  const prefersLight = window.matchMedia('(prefers-color-scheme: light)').matches
  const initial = prefersLight ? 'light' : 'dark'
  document.documentElement.setAttribute('data-theme', initial)
  return initial
}

export default function ThemeToggle() {
  const [theme, setTheme] = useState(
    () => document.documentElement.getAttribute('data-theme') || 'dark'
  )

  useEffect(() => {
    document.documentElement.setAttribute('data-theme', theme)
  }, [theme])

  const toggle = () => {
    const next = theme === 'dark' ? 'light' : 'dark'
    localStorage.setItem(STORAGE_KEY, next)
    setTheme(next)
  }

  // Icon indicates the theme you'd switch TO, not the one you're in —
  // matches iOS/macOS convention and is what most users expect.
  const icon = theme === 'dark' ? '☀' : '☾'
  const label = theme === 'dark' ? 'Switch to light mode' : 'Switch to dark mode'

  return (
    <button
      type="button"
      className="theme-toggle"
      onClick={toggle}
      aria-label={label}
      title={label}
    >
      {icon}
    </button>
  )
}
