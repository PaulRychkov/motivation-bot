import { useEffect, useRef, useState } from "react";

interface Message {
  role: "user" | "assistant";
  text: string;
  created_at: string;
}

interface HistoryMessage {
  role: string;
  content: string | null;
  created_at: string;
}

declare global {
  interface Window {
    go?: { main?: { App?: { GetBackendURL?: () => Promise<string>; GetToken?: () => Promise<string> } } };
  }
}

async function backendBase(): Promise<string> {
  const binding = window.go?.main?.App?.GetBackendURL;
  if (binding) return (await binding()).replace(/\/$/, "");
  const params = new URLSearchParams(window.location.search);
  const stored = window.localStorage.getItem("chat_base");
  return (params.get("base") ?? stored ?? "").replace(/\/$/, "");
}

async function token(): Promise<string> {
  const binding = window.go?.main?.App?.GetToken;
  if (binding) return await binding();
  const params = new URLSearchParams(window.location.search);
  const stored = window.localStorage.getItem("chat_token");
  return params.get("token") ?? stored ?? "";
}

export default function App() {
  const [messages, setMessages] = useState<Message[]>([]);
  const [draft, setDraft] = useState("");
  const [connected, setConnected] = useState(false);
  const wsRef = useRef<WebSocket | null>(null);
  const scrollRef = useRef<HTMLDivElement | null>(null);

  useEffect(() => {
    let closed = false;
    let ws: WebSocket | null = null;

    (async () => {
      const base = await backendBase();
      const tok = await token();
      const httpBase = base || window.location.origin;
      const q = tok ? `?token=${encodeURIComponent(tok)}` : "";

      fetch(`${httpBase}/api/v1/history?limit=50${tok ? `&token=${encodeURIComponent(tok)}` : ""}`)
        .then((r) => (r.ok ? r.json() : { messages: [] }))
        .then((data: { messages: HistoryMessage[] }) => {
          const loaded = (data.messages ?? [])
            .filter((m) => (m.role === "user" || m.role === "assistant") && m.content)
            .map((m) => ({ role: m.role as "user" | "assistant", text: m.content!, created_at: m.created_at }));
          setMessages(loaded);
        })
        .catch(() => undefined);

      const wsBase = httpBase.replace(/^http/, "ws");
      const connect = () => {
        if (closed) return;
        ws = new WebSocket(`${wsBase}/ws${q}`);
        wsRef.current = ws;
        ws.onopen = () => setConnected(true);
        ws.onclose = () => {
          setConnected(false);
          if (!closed) window.setTimeout(connect, 2000);
        };
        ws.onmessage = (ev) => {
          try {
            const msg = JSON.parse(ev.data) as { type: string; role: string; text: string; created_at: string };
            if (msg.type === "message") {
              setMessages((prev) => [...prev, { role: msg.role as "user" | "assistant", text: msg.text, created_at: msg.created_at }]);
            }
          } catch {
            /* ignore */
          }
        };
      };
      connect();
    })();

    return () => {
      closed = true;
      ws?.close();
    };
  }, []);

  useEffect(() => {
    scrollRef.current?.scrollTo({ top: scrollRef.current.scrollHeight, behavior: "smooth" });
  }, [messages]);

  const send = () => {
    const text = draft.trim();
    if (!text || !wsRef.current || wsRef.current.readyState !== WebSocket.OPEN) return;
    wsRef.current.send(JSON.stringify({ text }));
    setDraft("");
  };

  return (
    <div className="flex h-full flex-col bg-surface text-slate-100">
      <header className="flex items-center gap-3 border-b border-white/10 px-4 py-3">
        <div className="text-lg font-semibold">Мотиватор</div>
        <span className={"h-2.5 w-2.5 rounded-full " + (connected ? "bg-green-400" : "bg-slate-500")} />
        <span className="text-xs text-slate-400">{connected ? "на связи" : "переподключение…"}</span>
      </header>

      <div ref={scrollRef} className="flex-1 space-y-2 overflow-y-auto px-4 py-4">
        {messages.length === 0 && (
          <div className="mt-10 text-center text-sm text-slate-500">Напиши сообщение — бот ответит здесь.</div>
        )}
        {messages.map((m, i) => (
          <div key={i} className={"flex " + (m.role === "user" ? "justify-end" : "justify-start")}>
            <div
              className={
                "max-w-[80%] whitespace-pre-wrap rounded-2xl px-4 py-2 text-sm " +
                (m.role === "user" ? "bg-accent text-white" : "bg-bubble text-slate-100")
              }
            >
              {m.text}
            </div>
          </div>
        ))}
      </div>

      <footer className="border-t border-white/10 p-3">
        <div className="flex items-end gap-2">
          <textarea
            className="max-h-32 min-h-[44px] flex-1 resize-none rounded-2xl bg-bubble px-4 py-2.5 text-sm outline-none placeholder:text-slate-500"
            placeholder="Сообщение…"
            value={draft}
            onChange={(e) => setDraft(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Enter" && !e.shiftKey) {
                e.preventDefault();
                send();
              }
            }}
            rows={1}
          />
          <button
            className="rounded-2xl bg-accent px-5 py-2.5 text-sm font-medium text-white disabled:opacity-40"
            onClick={send}
            disabled={!draft.trim() || !connected}
          >
            Отправить
          </button>
        </div>
      </footer>
    </div>
  );
}
