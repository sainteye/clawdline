import { createContext, useContext } from "react"

/** Account authority for the signed-in hosted browser; absent in the local console. */
export interface CloudAccount {
  apiOrigin: string
  deviceID: string
}

export const CloudAccountContext = createContext<CloudAccount | null>(null)

export function useCloudAccount(): CloudAccount | null {
  return useContext(CloudAccountContext)
}
