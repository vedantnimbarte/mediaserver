import React from 'react'
import ReactDOM from 'react-dom/client'
import { BrowserRouter } from 'react-router-dom'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'

import App from './App'
import { AuthProvider } from './lib/auth'
import { PrefsProvider } from './lib/prefs'
import { ToastProvider } from './components/Toast'
import { ConfirmProvider } from './components/Modal'
import { ShortcutsProvider } from './components/Shortcuts'
import './index.css'

const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      refetchOnWindowFocus: false,
      staleTime: 30_000,
      retry: (failureCount, error: any) => {
        if (error?.status === 401 || error?.status === 403) return false
        return failureCount < 2
      },
    },
  },
})

ReactDOM.createRoot(document.getElementById('root')!).render(
  <React.StrictMode>
    <QueryClientProvider client={queryClient}>
      <BrowserRouter>
        {/* Auth is outermost: preferences and everything below depend on knowing who
            the viewer is. Toasts sit above the router so any page can raise one. */}
        <AuthProvider>
          <ToastProvider>
            <ConfirmProvider>
              <PrefsProvider>
                <ShortcutsProvider>
                  <App />
                </ShortcutsProvider>
              </PrefsProvider>
            </ConfirmProvider>
          </ToastProvider>
        </AuthProvider>
      </BrowserRouter>
    </QueryClientProvider>
  </React.StrictMode>,
)
