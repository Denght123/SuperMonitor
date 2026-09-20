import { createContext, useContext } from 'react'

export type AuthenticationContextValue = {
  required: boolean
  logout: () => Promise<void>
}

export const AuthenticationContext = createContext<AuthenticationContextValue>({
  required: false,
  logout: async () => undefined,
})

export function useAuthentication() {
  return useContext(AuthenticationContext)
}
