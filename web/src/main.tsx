import React from 'react'
import ReactDOM from 'react-dom/client'
import '@fontsource-variable/manrope'
import './styles.css'
import { App } from './App'
import { ToastProvider } from './components/Toast'
import { AuthGate } from './components/AuthGate'

ReactDOM.createRoot(document.getElementById('root')!).render(
  <React.StrictMode>
    <ToastProvider>
      <AuthGate><App /></AuthGate>
    </ToastProvider>
  </React.StrictMode>,
)
