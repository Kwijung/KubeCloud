import { useQuery } from "@tanstack/react-query";
import { FitAddon } from "@xterm/addon-fit";
import { Terminal } from "@xterm/xterm";
import "@xterm/xterm/css/xterm.css";
import { ArrowLeftRight, ClipboardPaste, Download, ExternalLink, FileText, Plug, RefreshCw, Unplug } from "lucide-react";
import { useCallback, useEffect, useRef, useState } from "react";
import { useParams } from "react-router-dom";
import { api, CONSOLE_REPLACED_CLOSE_CODE, consoleStreamURL } from "../api";
import { lastProjectId } from "../app/Layout";
import { useI18n } from "../i18n";
import { Alert, Badge, Button, Card, Empty, Spinner } from "../ui";

type ConsoleState = "checking" | "busy" | "connecting" | "connected" | "closed" | "replaced" | "unavailable";

// controlKey maps Ctrl+letter to the terminal control character using the physical key. With a Korean or
// other IME active, the browser reports Ctrl+C as event.key "ㅊ" or "Process", which xterm cannot translate.
export function controlKey(event: KeyboardEvent): string {
  if (!event.ctrlKey || event.metaKey || event.altKey) {
    return "";
  }
  const letter = /^Key([A-Z])$/.exec(event.code);
  if (letter) {
    return String.fromCharCode(letter[1].charCodeAt(0) - 64);
  }
  const symbols: Record<string, string> = { BracketLeft: "\x1b", Backslash: "\x1c", BracketRight: "\x1d", Space: "\x00" };
  return symbols[event.code] ?? "";
}

const specialKeys: Array<{ label: string; value: string }> = [
  { label: "Ctrl+C", value: "\x03" },
  { label: "Ctrl+D", value: "\x04" },
  { label: "Ctrl+Z", value: "\x1a" },
  { label: "Esc", value: "\x1b" },
  { label: "Tab", value: "\t" },
];

export function SerialConsole({
  projectId,
  vmId,
  running,
  onShowBootLog,
  standalone,
}: {
  projectId: string;
  vmId: string;
  running: boolean;
  onShowBootLog?: () => void;
  standalone?: boolean;
}) {
  const { t } = useI18n();
  const hostRef = useRef<HTMLDivElement>(null);
  const termRef = useRef<Terminal | null>(null);
  const socketRef = useRef<WebSocket | null>(null);
  const [state, setState] = useState<ConsoleState>("checking");
  const [pasteError, setPasteError] = useState(false);

  const send = useCallback((value: string) => {
    const socket = socketRef.current;
    if (socket && socket.readyState === WebSocket.OPEN) {
      socket.send(new TextEncoder().encode(value));
    }
  }, []);

  // One terminal per mounted console; the socket comes and goes with connect/disconnect.
  useEffect(() => {
    if (!hostRef.current) {
      return;
    }
    const term = new Terminal({
      convertEol: false,
      cursorBlink: true,
      fontFamily: '"JetBrains Mono", "SFMono-Regular", Menlo, Consolas, monospace',
      fontSize: 13,
      scrollback: 5000,
      theme: { background: "#0f1720", foreground: "#d7e0ea" },
    });
    const fit = new FitAddon();
    term.loadAddon(fit);
    term.open(hostRef.current);
    term.attachCustomKeyEventHandler((event) => {
      const control = event.type === "keydown" ? controlKey(event) : "";
      if (control) {
        event.preventDefault();
        send(control);
        return false;
      }
      return true;
    });
    const input = term.onData((value) => send(value));
    termRef.current = term;
    const resize = new ResizeObserver(() => {
      try {
        fit.fit();
      } catch {
        // The host is hidden (e.g. a collapsed tab); fit again when it is shown.
      }
    });
    resize.observe(hostRef.current);
    return () => {
      resize.disconnect();
      input.dispose();
      term.dispose();
      termRef.current = null;
    };
  }, [send]);

  const disconnect = useCallback(() => {
    const socket = socketRef.current;
    socketRef.current = null;
    socket?.close();
    setState("closed");
  }, []);

  const connect = useCallback(() => {
    socketRef.current?.close();
    setState("connecting");
    const term = termRef.current;
    term?.reset();
    const socket = new WebSocket(consoleStreamURL(projectId, vmId));
    socket.binaryType = "arraybuffer";
    socketRef.current = socket;
    const decoder = new TextDecoder();
    socket.onopen = () => {
      if (socketRef.current === socket) {
        setState("connected");
        term?.focus();
        // Wake the getty up so the login prompt shows without the user pressing Enter.
        send("\r");
      }
    };
    socket.onmessage = (event) => {
      if (typeof event.data === "string") {
        term?.write(event.data);
      } else {
        term?.write(decoder.decode(new Uint8Array(event.data as ArrayBuffer), { stream: true }));
      }
    };
    socket.onclose = (event) => {
      if (socketRef.current !== socket) {
        return; // a newer connection replaced this one, or the user disconnected
      }
      socketRef.current = null;
      setState(event.code === CONSOLE_REPLACED_CLOSE_CODE ? "replaced" : "closed");
    };
  }, [projectId, vmId, send]);

  // On open, connect right away unless someone else holds the console: taking it over must be deliberate.
  const capability = useQuery({
    queryKey: ["console", projectId, vmId],
    queryFn: () => api.console(projectId, vmId),
    enabled: running,
    staleTime: 0,
    gcTime: 0,
    refetchOnWindowFocus: false,
  });
  // Connect automatically once per run of the VM. After a disconnect or a takeover, reconnecting is the
  // user's call; otherwise two open consoles would keep taking the console from each other.
  const autoConnected = useRef(false);
  // When the VM's current run started, as seen here. A capability read before it (cached from the previous
  // run) must not decide the connection: it would say the console is free when another tab has it now.
  const runSeenAt = useRef(Date.now());
  useEffect(() => {
    if (!running) {
      socketRef.current?.close();
      socketRef.current = null;
      autoConnected.current = false;
      runSeenAt.current = Number.POSITIVE_INFINITY;
      setState("unavailable");
      return;
    }
    if (runSeenAt.current === Number.POSITIVE_INFINITY) {
      runSeenAt.current = Date.now();
    }
    if (!capability.data || autoConnected.current || capability.dataUpdatedAt < runSeenAt.current) {
      return;
    }
    autoConnected.current = true;
    if (!capability.data.available) {
      setState("unavailable");
    } else if (capability.data.inUse) {
      setState("busy");
    } else {
      connect();
    }
  }, [running, capability.data, capability.dataUpdatedAt, connect]);

  useEffect(() => () => socketRef.current?.close(), []);

  async function paste() {
    setPasteError(false);
    try {
      const text = await navigator.clipboard.readText();
      send(text.replace(/\r?\n/g, "\r"));
      termRef.current?.focus();
    } catch {
      setPasteError(true);
    }
  }

  const badge =
    state === "connected" ? (
      <Badge tone="success" dot>
        {t.consoleConnected}
      </Badge>
    ) : state === "connecting" || state === "checking" ? (
      <Badge tone="info" dot>
        {t.consoleConnecting}
      </Badge>
    ) : state === "busy" ? (
      <Badge tone="warning" dot>
        {t.consoleBusy}
      </Badge>
    ) : state === "replaced" ? (
      <Badge tone="warning" dot>
        {t.consoleReplaced}
      </Badge>
    ) : (
      <Badge dot>{t.consoleDisconnected}</Badge>
    );

  const windowURL = `/console/${projectId}/${vmId}`;
  const showTerminal = state === "connecting" || state === "connected" || state === "closed" || state === "replaced";

  return (
    <Card
      title={
        <span className="row">
          {t.consoleTitle} {badge}
        </span>
      }
      description={t.consoleHint}
      actions={
        <>
          <Button size="sm" icon={ClipboardPaste} disabled={state !== "connected"} onClick={paste}>
            {t.consolePaste}
          </Button>
          {state === "connected" || state === "connecting" ? (
            <Button size="sm" icon={Unplug} onClick={disconnect}>
              {t.consoleDisconnect}
            </Button>
          ) : (
            <Button size="sm" icon={state === "busy" ? Plug : RefreshCw} disabled={!running} onClick={connect}>
              {state === "closed" || state === "replaced" ? t.consoleReconnect : t.consoleConnect}
            </Button>
          )}
          {!standalone && (
            <Button
              size="sm"
              variant="ghost"
              icon={ExternalLink}
              onClick={() => {
                disconnect();
                window.open(windowURL, `console-${vmId}`, "width=1000,height=640");
              }}
            >
              {t.consoleOpenWindow}
            </Button>
          )}
        </>
      }
      flush
    >
      {pasteError && (
        <div style={{ padding: "12px 16px 0" }}>
          <Alert tone="warning">{t.consolePasteFailed}</Alert>
        </div>
      )}
      {state === "busy" && (
        <div className="busy-panel">
          <ArrowLeftRight size={28} color="var(--c-warning)" aria-hidden />
          <strong>{t.consoleBusyTitle}</strong>
          <p>{t.consoleBusyBody}</p>
          <div className="row">
            <Button variant="primary" icon={Plug} onClick={connect}>
              {t.consoleTakeOver}
            </Button>
            {onShowBootLog && (
              <Button icon={FileText} onClick={onShowBootLog}>
                {t.bootLogShow}
              </Button>
            )}
          </div>
        </div>
      )}
      {state === "unavailable" && <Empty title={t.consoleUnavailable} />}
      {state === "checking" && <Empty title={<Spinner label={t.loading} />} />}
      <div className="console-pane" style={{ display: showTerminal ? "flex" : "none" }}>
        <div className="key-strip">
          <span className="muted small">{t.consoleSpecialKeys}</span>
          {specialKeys.map((key) => (
            <Button key={key.label} size="sm" disabled={state !== "connected"} onClick={() => send(key.value)}>
              {key.label}
            </Button>
          ))}
        </div>
        <div className="terminal-host" ref={hostRef} data-testid="terminal" />
      </div>
    </Card>
  );
}

export function BootLog({ projectId, vmId, running }: { projectId: string; vmId: string; running: boolean }) {
  const { t, errorText } = useI18n();
  const preRef = useRef<HTMLPreElement>(null);
  const log = useQuery({
    queryKey: ["boot-log", projectId, vmId],
    queryFn: () => api.bootLog(projectId, vmId),
    enabled: running,
    retry: false,
  });
  const text = log.data?.log ?? "";
  useEffect(() => {
    const pre = preRef.current;
    if (pre) {
      pre.scrollTop = pre.scrollHeight;
    }
  }, [text]);

  function download() {
    const url = URL.createObjectURL(new Blob([text], { type: "text/plain" }));
    const link = document.createElement("a");
    link.href = url;
    link.download = `${vmId}-boot.log`;
    link.click();
    URL.revokeObjectURL(url);
  }

  return (
    <Card
      title={t.bootLogTitle}
      description={t.bootLogDescription}
      actions={
        <>
          <Button size="sm" icon={RefreshCw} disabled={!running} loading={log.isFetching} onClick={() => log.refetch()}>
            {t.refresh}
          </Button>
          <Button size="sm" icon={Download} disabled={!text} onClick={download}>
            {t.bootLogDownload}
          </Button>
        </>
      }
      flush
    >
      {!running ? (
        <Empty title={t.bootLogUnavailable} />
      ) : log.isError ? (
        <div style={{ padding: 16 }}>
          <Alert tone="warning">{errorText(log.error)}</Alert>
        </div>
      ) : (
        <pre className="boot-log" ref={preRef} data-testid="boot-log">
          {log.isLoading ? t.loading : stripAnsi(text) || t.bootLogEmpty}
        </pre>
      )}
    </Card>
  );
}

// stripAnsi removes terminal escape sequences and carriage returns so the boot log reads as plain text.
function stripAnsi(text: string): string {
  // eslint-disable-next-line no-control-regex
  return text.replace(/\x1b\[[0-9;?]*[ -/]*[@-~]/g, "").replace(/\r/g, "");
}

// ConsoleWindow is the console opened in its own browser window, without the console chrome.
export function ConsoleWindow() {
  const params = useParams();
  const projectId = params.projectId ?? lastProjectId();
  const vmId = params.vmId ?? "";
  const { t } = useI18n();
  const detail = useQuery({ queryKey: ["vm", projectId, vmId], queryFn: () => api.vm(projectId, vmId) });
  useEffect(() => {
    document.title = `${detail.data?.virtualMachine.name ?? vmId} · ${t.consoleTitle}`;
  }, [detail.data, vmId, t]);
  if (detail.isLoading) {
    return <Empty title={<Spinner label={t.loading} />} />;
  }
  return (
    <div className="console-window">
      <SerialConsole projectId={projectId} vmId={vmId} running={detail.data?.runtimeStatus === "Running"} standalone />
    </div>
  );
}
