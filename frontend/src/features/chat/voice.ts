// WebRTC controller for the Talk tab. Audio flows browser ↔ OpenAI
// directly; the Go backend only mints the ephemeral client_secret and
// receives finalized transcript turns as POSTed chat_messages rows.
//
// Lifecycle:
//   idle → connecting → live → ending → idle
// Errors flip to 'idle' with a lastError exposed.

import { appendVoiceTranscript, startVoiceSession } from "./api";

export type VoiceStatus =
  | "idle"
  | "connecting"
  | "live"
  | "ending";

export interface VoiceListener {
  onStatusChange?: (status: VoiceStatus) => void;
  onError?: (msg: string) => void;
  // onTranscript fires once per finalized turn (after the controller has
  // already POSTed it to the backend). The cache layer doesn't need this
  // because consumers invalidate on each turn — but the TalkPanel uses
  // it to scroll-into-view + animate the new bubble before the refetch
  // lands.
  onTranscript?: (turn: { role: "user" | "assistant"; content: string }) => void;
  onCrisis?: (resourcesUrl: string) => void;
}

// OpenAI Realtime data-channel event shapes. We only need a tiny subset
// — the *.completed and *.done events that carry finalized text.
interface RealtimeEvent {
  type?: string;
  transcript?: string;
  // Some events nest the text inside `item.content[].transcript`.
  item?: {
    content?: { transcript?: string; text?: string }[];
  };
  // For response.output_text.done.
  text?: string;
}

// GA Realtime SDP exchange endpoint. The model is baked into the
// ephemeral client_secret's session config (see backend
// realtime.MintEphemeralSecret) — no `?model=` query param.
const REALTIME_CALLS_URL = "https://api.openai.com/v1/realtime/calls";

// VoiceController owns the RTCPeerConnection + DataChannel for one Talk
// session. Construct once; call start()/stop() as the user toggles the
// mic. Singleton-ish — only one live call at a time per tab.
export class VoiceController {
  private status: VoiceStatus = "idle";
  private pc: RTCPeerConnection | null = null;
  private dc: RTCDataChannel | null = null;
  private localStream: MediaStream | null = null;
  private remoteAudio: HTMLAudioElement | null = null;
  private clientSeq = 0;
  private sessionId: string | null = null;
  private listeners: VoiceListener;
  private lastError: string | null = null;

  constructor(listeners: VoiceListener = {}) {
    this.listeners = listeners;
  }

  getStatus(): VoiceStatus {
    return this.status;
  }
  getLastError(): string | null {
    return this.lastError;
  }

  private setStatus(next: VoiceStatus) {
    if (this.status === next) return;
    this.status = next;
    this.listeners.onStatusChange?.(next);
  }

  private fail(msg: string) {
    this.lastError = msg;
    this.listeners.onError?.(msg);
    void this.stop();
  }

  // start mints the ephemeral secret, opens the WebRTC peer to OpenAI,
  // attaches the mic + remote-audio elements, and listens to the data
  // channel for transcript events. Throws nothing — errors surface via
  // onError + status flip back to 'idle'.
  async start(sessionId: string): Promise<void> {
    if (this.status !== "idle") return;
    this.lastError = null;
    this.sessionId = sessionId;
    this.setStatus("connecting");

    let mint;
    try {
      mint = await startVoiceSession(sessionId);
    } catch (err) {
      this.fail(err instanceof Error ? err.message : "voice not available");
      return;
    }

    // Mic capture. Permission denied throws here; surface a friendly
    // message rather than the raw DOMException string.
    let stream: MediaStream;
    try {
      stream = await navigator.mediaDevices.getUserMedia({ audio: true });
    } catch {
      this.fail("Microphone permission was denied");
      return;
    }
    this.localStream = stream;

    const pc = new RTCPeerConnection();
    this.pc = pc;

    // Pipe model audio into a hidden <audio> element so the user can
    // hear replies. Created lazily; cleaned up on stop.
    pc.ontrack = (event) => {
      const [remote] = event.streams;
      if (!remote) return;
      if (!this.remoteAudio) {
        const el = document.createElement("audio");
        el.autoplay = true;
        el.srcObject = remote;
        this.remoteAudio = el;
      } else {
        this.remoteAudio.srcObject = remote;
      }
    };

    for (const track of stream.getAudioTracks()) {
      pc.addTrack(track, stream);
    }

    // Data channel for transcript events. OpenAI Realtime documents the
    // channel name as "oai-events".
    const dc = pc.createDataChannel("oai-events");
    this.dc = dc;
    dc.onmessage = (e) => this.handleDataChannelMessage(e);

    pc.onconnectionstatechange = () => {
      if (!this.pc) return;
      const s = this.pc.connectionState;
      if (s === "failed" || s === "disconnected" || s === "closed") {
        if (this.status === "live") {
          // Treat as user-initiated stop; we don't auto-retry here.
          void this.stop();
        }
      }
    };

    let offer: RTCSessionDescriptionInit;
    try {
      offer = await pc.createOffer();
      await pc.setLocalDescription(offer);
    } catch (err) {
      this.fail(err instanceof Error ? err.message : "could not create offer");
      return;
    }

    // POST our SDP offer to OpenAI; receive the answer SDP. The
    // ephemeral client_secret is the bearer for this single exchange.
    let answerSdp: string;
    try {
      const res = await fetch(REALTIME_CALLS_URL, {
        method: "POST",
        headers: {
          Authorization: `Bearer ${mint.client_secret}`,
          "Content-Type": "application/sdp",
        },
        body: offer.sdp ?? "",
      });
      if (!res.ok) {
        const text = await res.text();
        throw new Error(`OpenAI ${res.status}: ${text.slice(0, 200)}`);
      }
      answerSdp = await res.text();
    } catch (err) {
      this.fail(err instanceof Error ? err.message : "OpenAI handshake failed");
      return;
    }

    try {
      await pc.setRemoteDescription({ type: "answer", sdp: answerSdp });
    } catch (err) {
      this.fail(err instanceof Error ? err.message : "could not set remote SDP");
      return;
    }

    this.setStatus("live");
  }

  // stop tears down the peer + tracks. Idempotent — safe to call from
  // status='live' or after an error flip.
  async stop(): Promise<void> {
    if (this.status === "idle") return;
    this.setStatus("ending");
    try {
      this.dc?.close();
    } catch {
      /* ignore */
    }
    try {
      this.pc?.getSenders().forEach((s) => {
        try {
          s.track?.stop();
        } catch {
          /* ignore */
        }
      });
      this.pc?.close();
    } catch {
      /* ignore */
    }
    try {
      this.localStream?.getTracks().forEach((t) => t.stop());
    } catch {
      /* ignore */
    }
    if (this.remoteAudio) {
      try {
        this.remoteAudio.srcObject = null;
      } catch {
        /* ignore */
      }
      this.remoteAudio = null;
    }
    this.pc = null;
    this.dc = null;
    this.localStream = null;
    this.sessionId = null;
    this.setStatus("idle");
  }

  // handleDataChannelMessage parses one OpenAI Realtime event. We only
  // care about *.completed (user transcript) and *.done (assistant
  // transcript). Both arrive after the audio is finalized.
  private handleDataChannelMessage(e: MessageEvent) {
    let evt: RealtimeEvent;
    try {
      evt = JSON.parse(typeof e.data === "string" ? e.data : new TextDecoder().decode(e.data));
    } catch {
      return;
    }
    if (!evt.type) return;

    // User audio transcribed. Schema (Dec 2025):
    //   conversation.item.input_audio_transcription.completed
    //     { transcript: "..." }
    if (evt.type.endsWith("input_audio_transcription.completed")) {
      const text = (evt.transcript ?? "").trim();
      if (text) this.persistTurn("user", text);
      return;
    }

    // Assistant audio transcript completed.
    //   response.audio_transcript.done { transcript: "..." }
    if (evt.type === "response.audio_transcript.done") {
      const text = (evt.transcript ?? "").trim();
      if (text) this.persistTurn("assistant", text);
      return;
    }

    // Some models emit response.output_text.done instead (text-only
    // response on a voice channel — rare but defensible to capture).
    if (evt.type === "response.output_text.done") {
      const text = (evt.text ?? "").trim();
      if (text) this.persistTurn("assistant", text);
      return;
    }

    // Fallback: response.done sometimes carries the final text in
    // item.content. Drop on the floor — a *.done event for the same
    // turn already fired above.
  }

  private persistTurn(role: "user" | "assistant", content: string) {
    if (!this.sessionId) return;
    this.clientSeq += 1;
    const seq = this.clientSeq;
    this.listeners.onTranscript?.({ role, content });
    void appendVoiceTranscript(this.sessionId, { role, content, client_seq: seq })
      .then((resp) => {
        if (resp.crisis && resp.resources_url) {
          this.listeners.onCrisis?.(resp.resources_url);
          void this.stop();
        }
      })
      .catch((err) => {
        // Transcript persistence is best-effort — log but don't tear
        // down the call; the user is still mid-conversation.
        // eslint-disable-next-line no-console
        console.warn("voice transcript persist failed", err);
      });
  }
}
