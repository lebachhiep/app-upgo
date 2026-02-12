import { useEffect, useRef, useCallback, useState } from 'react'
import { AppService, RuntimeService } from '@/services/wails'

const TERMINAL_COLORS = {
  background: '#0C0C0C',
  foreground: '#CCCCCC',
  cursor: '#CCCCCC',
  black: '#0C0C0C',
  red: '#C50F1F',
  green: '#13A10E',
  yellow: '#C19C00',
  blue: '#0037DA',
  magenta: '#881798',
  cyan: '#3A96DD',
  white: '#CCCCCC',
  brightBlack: '#767676',
  brightRed: '#E74856',
  brightGreen: '#16C60C',
  brightYellow: '#F9F1A5',
  brightBlue: '#3B78FF',
  brightMagenta: '#B4009E',
  brightCyan: '#61D6D6',
  brightWhite: '#F2F2F2',
}

function Terminal() {
  const termRef = useRef<HTMLDivElement>(null)
  const xtermRef = useRef<import('xterm').Terminal | null>(null)
  const fitAddonRef = useRef<import('xterm-addon-fit').FitAddon | null>(null)
  const [history, setHistory] = useState<string[]>([])
  const historyIndexRef = useRef(-1)
  const currentLineRef = useRef('')

  const writePrompt = useCallback(() => {
    xtermRef.current?.write('\r\n\x1b[36mbnc>\x1b[0m ')
  }, [])

  const handleCommand = useCallback(async (cmd: string) => {
    const trimmed = cmd.trim()
    if (!trimmed) {
      writePrompt()
      return
    }

    setHistory(prev => [...prev, trimmed])
    historyIndexRef.current = -1

    if (trimmed === 'clear') {
      xtermRef.current?.clear()
      writePrompt()
      return
    }

    if (trimmed === 'help') {
      xtermRef.current?.writeln('')
      xtermRef.current?.writeln('\x1b[33mAvailable Commands:\x1b[0m')
      xtermRef.current?.writeln('  start      Start the BNC node')
      xtermRef.current?.writeln('  stop       Stop the BNC node')
      xtermRef.current?.writeln('  status     Show node status')
      xtermRef.current?.writeln('  stats      Show node statistics')
      xtermRef.current?.writeln('  config     Manage configuration')
      xtermRef.current?.writeln('  version    Show version information')
      xtermRef.current?.writeln('  device-id  Show device ID')
      xtermRef.current?.writeln('  proxy      Manage proxies')
      xtermRef.current?.writeln('  clear      Clear terminal')
      xtermRef.current?.writeln('  help       Show this help')
      writePrompt()
      return
    }

    try {
      const result = await AppService.ExecuteCommand(trimmed)
      if (result) {
        xtermRef.current?.writeln('')
        const lines = result.split('\n')
        for (const line of lines) {
          xtermRef.current?.writeln(line)
        }
      }
    } catch (err) {
      xtermRef.current?.writeln('')
      xtermRef.current?.writeln(`\x1b[31mError: ${err}\x1b[0m`)
    }
    writePrompt()
  }, [writePrompt])

  useEffect(() => {
    let mounted = true

    const initTerminal = async () => {
      const { Terminal: XTerminal } = await import('xterm')
      const { FitAddon } = await import('xterm-addon-fit')
      await import('xterm/css/xterm.css')

      if (!mounted || !termRef.current) return

      const fitAddon = new FitAddon()
      fitAddonRef.current = fitAddon

      const term = new XTerminal({
        fontFamily: "'Cascadia Code', 'Consolas', 'Courier New', monospace",
        fontSize: 13,
        lineHeight: 1.2,
        cursorBlink: true,
        cursorStyle: 'bar',
        theme: TERMINAL_COLORS,
        allowTransparency: true,
        scrollback: 5000,
      })

      term.loadAddon(fitAddon)
      term.open(termRef.current)

      try { fitAddon.fit() } catch { /* */ }

      xtermRef.current = term

      term.writeln('\x1b[36m╔══════════════════════════════════════╗\x1b[0m')
      term.writeln('\x1b[36m║          UPGO Node Terminal          ║\x1b[0m')
      term.writeln('\x1b[36m╚══════════════════════════════════════╝\x1b[0m')
      term.writeln('')
      term.writeln('Type \x1b[33mhelp\x1b[0m for available commands.')
      writePrompt()

      let currentInput = ''

      term.onKey(({ key, domEvent }) => {
        const ev = domEvent

        if (ev.ctrlKey && ev.key === 'l') {
          term.clear(); writePrompt(); currentInput = ''; return
        }
        if (ev.ctrlKey && ev.key === 'c') {
          term.write('^C'); currentInput = ''; writePrompt(); return
        }
        if (ev.key === 'Enter') {
          handleCommand(currentInput); currentLineRef.current = ''; currentInput = ''; return
        }
        if (ev.key === 'Backspace') {
          if (currentInput.length > 0) { currentInput = currentInput.slice(0, -1); term.write('\b \b') }
          return
        }
        if (ev.key === 'ArrowUp') {
          const hist = history
          if (hist.length > 0) {
            if (historyIndexRef.current === -1) {
              currentLineRef.current = currentInput; historyIndexRef.current = hist.length - 1
            } else if (historyIndexRef.current > 0) { historyIndexRef.current-- }
            const entry = hist[historyIndexRef.current]
            term.write('\b \b'.repeat(currentInput.length)); term.write(entry); currentInput = entry
          }
          return
        }
        if (ev.key === 'ArrowDown') {
          const hist = history
          if (historyIndexRef.current >= 0) {
            historyIndexRef.current++
            const entry = historyIndexRef.current >= hist.length
              ? (historyIndexRef.current = -1, currentLineRef.current)
              : hist[historyIndexRef.current]
            term.write('\b \b'.repeat(currentInput.length)); term.write(entry); currentInput = entry
          }
          return
        }
        if (key.length === 1 && !ev.ctrlKey && !ev.altKey && !ev.metaKey) {
          currentInput += key; term.write(key)
        }
      })

      const handleResize = () => { try { fitAddon.fit() } catch { /* */ } }
      window.addEventListener('resize', handleResize)

      const cleanup = RuntimeService.EventsOn('log:new', (msg: unknown) => {
        term.writeln(`\r\n\x1b[90m[LOG]\x1b[0m ${msg}`)
      })

      return () => {
        window.removeEventListener('resize', handleResize)
        if (cleanup) cleanup()
        term.dispose()
      }
    }

    const cleanupPromise = initTerminal()
    return () => { mounted = false; cleanupPromise.then(c => c?.()) }
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  return (
    <div style={containerStyles.wrapper}>
      <div style={containerStyles.header}>UPGO Node CLI</div>
      <div style={containerStyles.terminal} ref={termRef} />
    </div>
  )
}

const containerStyles: Record<string, React.CSSProperties> = {
  wrapper: {
    height: '100%',
    display: 'flex',
    flexDirection: 'column',
    backgroundColor: '#0C0C0C',
    borderRadius: 8,
    overflow: 'hidden',
  },
  header: {
    padding: '8px 12px',
    fontSize: 11,
    color: '#666',
    borderBottom: '1px solid #1E1E1E',
    fontFamily: "'Cascadia Code', 'Consolas', monospace",
    textTransform: 'uppercase',
    letterSpacing: 0.5,
  },
  terminal: {
    flex: 1,
    padding: 0,
  },
}

export default Terminal
