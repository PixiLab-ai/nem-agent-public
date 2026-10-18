/// <reference types="vite/client" />
import type { OverlayApi } from '../preload/overlay'

declare global {
  interface Window { nem: OverlayApi }
}

declare module '*.vue' {
  import type { DefineComponent } from 'vue'
  const component: DefineComponent<object, object, unknown>
  export default component
}
