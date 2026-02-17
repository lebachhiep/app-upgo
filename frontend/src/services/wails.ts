import type { RelayStatus, Config, PlatformInfo, VersionInfo, ProxyStatus } from '@/types'

declare global {
  interface Window {
    go: {
      main: {
        App: {
          StartRelay(partnerId: string): Promise<void>
          StopRelay(): Promise<void>
          GetStatus(): Promise<RelayStatus>
          IsRelayRunning(): Promise<boolean>
          GetConfig(): Promise<Config>
          SetConfigValue(key: string, value: string): Promise<void>
          GetConfigValue(key: string): Promise<string>
          AddProxy(proxyUrl: string): Promise<void>
          RemoveProxy(proxyUrl: string): Promise<void>
          RemoveAllProxies(): Promise<void>
          GetProxies(): Promise<string[]>
          ExecuteCommand(cmdStr: string): Promise<string>
          GetLogs(): Promise<string[]>
          ClearLogs(): Promise<void>
          GetPlatformInfo(): Promise<PlatformInfo>
          GetVersion(): Promise<VersionInfo>
          SetLaunchOnStartup(enabled: boolean): Promise<void>
          GetLaunchOnStartup(): Promise<boolean>
          QuitApp(): Promise<void>
          CloseWindow(): Promise<void>
          IsWindowMaximised(): Promise<boolean>
          CheckProxy(proxyUrl: string): Promise<ProxyStatus>
          CheckAllProxies(): Promise<ProxyStatus[]>
          GetEntryLogs(idx: number): Promise<string[]>
        }
      }
    }
    runtime: {
      EventsOn(eventName: string, callback: (...args: unknown[]) => void): () => void
      EventsOff(eventName: string): void
      WindowMinimise(): void
      WindowMaximise(): void
      WindowUnmaximise(): void
      WindowToggleMaximise(): void
      Quit(): void
    }
  }
}

export const AppService = {
  StartRelay: (partnerId: string) => window.go?.main?.App?.StartRelay(partnerId),
  StopRelay: () => window.go?.main?.App?.StopRelay(),
  GetStatus: () => window.go?.main?.App?.GetStatus(),
  IsRelayRunning: () => window.go?.main?.App?.IsRelayRunning(),
  GetConfig: () => window.go?.main?.App?.GetConfig(),
  SetConfigValue: (key: string, value: string) => window.go?.main?.App?.SetConfigValue(key, value),
  GetConfigValue: (key: string) => window.go?.main?.App?.GetConfigValue(key),
  AddProxy: (proxyUrl: string) => window.go?.main?.App?.AddProxy(proxyUrl),
  RemoveProxy: (proxyUrl: string) => window.go?.main?.App?.RemoveProxy(proxyUrl),
  RemoveAllProxies: () => window.go?.main?.App?.RemoveAllProxies(),
  GetProxies: () => window.go?.main?.App?.GetProxies(),
  ExecuteCommand: (cmdStr: string) => window.go?.main?.App?.ExecuteCommand(cmdStr),
  GetLogs: () => window.go?.main?.App?.GetLogs(),
  ClearLogs: () => window.go?.main?.App?.ClearLogs(),
  GetPlatformInfo: () => window.go?.main?.App?.GetPlatformInfo(),
  GetVersion: () => window.go?.main?.App?.GetVersion(),
  SetLaunchOnStartup: (enabled: boolean) => window.go?.main?.App?.SetLaunchOnStartup(enabled),
  GetLaunchOnStartup: () => window.go?.main?.App?.GetLaunchOnStartup(),
  QuitApp: () => window.go?.main?.App?.QuitApp(),
  CloseWindow: () => window.go?.main?.App?.CloseWindow(),
  IsWindowMaximised: () => window.go?.main?.App?.IsWindowMaximised(),
  CheckProxy: (proxyUrl: string) => window.go?.main?.App?.CheckProxy(proxyUrl),
  CheckAllProxies: () => window.go?.main?.App?.CheckAllProxies(),
  GetEntryLogs: (idx: number) => window.go?.main?.App?.GetEntryLogs(idx),
}

export const RuntimeService = {
  EventsOn: (event: string, callback: (...args: unknown[]) => void) =>
    window.runtime?.EventsOn(event, callback),
  EventsOff: (event: string) => window.runtime?.EventsOff(event),
  WindowMinimise: () => window.runtime?.WindowMinimise(),
  WindowMaximise: () => window.runtime?.WindowMaximise(),
  WindowToggleMaximise: () => window.runtime?.WindowToggleMaximise(),
  Quit: () => window.runtime?.Quit(),
}
