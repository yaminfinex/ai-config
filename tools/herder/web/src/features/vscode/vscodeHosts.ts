import { useSyncExternalStore } from 'react'
import { createVSCodeHostsStore, vscodeHostKey } from './vscodeModel.ts'

export const vscodeHosts = createVSCodeHostsStore(
  () => (typeof window === 'undefined' ? null : window.localStorage),
  typeof window === 'undefined' ? null : window,
)

export function useVSCodeHosts() {
  return useSyncExternalStore(vscodeHosts.subscribe, vscodeHosts.get, vscodeHosts.get)
}

export function currentHostKey() {
  return vscodeHostKey(window.location)
}
