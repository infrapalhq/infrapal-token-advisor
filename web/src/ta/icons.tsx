// Small line icons. Paths are from Lucide (https://lucide.dev, ISC licence),
// inlined so the dashboard makes no outside requests.
import type { ReactNode } from "react";
import markUrl from "../assets/infrapal-mark.svg";
import claudeUrl from "../assets/claude.png";
import { isClaude } from "./format";

function Svg({ size = 16, children, className }: { size?: number; children: ReactNode; className?: string }) {
  return (
    <svg className={`ico${className ? ` ${className}` : ""}`} width={size} height={size} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={1.8} strokeLinecap="round" strokeLinejoin="round" aria-hidden="true" focusable="false">
      {children}
    </svg>
  );
}

type P = { size?: number; className?: string };

export const IconGrid = (p: P) => (
  <Svg {...p}><rect x="3" y="3" width="7" height="7" rx="1.5" /><rect x="14" y="3" width="7" height="7" rx="1.5" /><rect x="14" y="14" width="7" height="7" rx="1.5" /><rect x="3" y="14" width="7" height="7" rx="1.5" /></Svg>
);
export const IconChat = (p: P) => <Svg {...p}><path d="M7.9 20A9 9 0 1 0 4 16.1L2 22Z" /></Svg>;
export const IconWrench = (p: P) => (
  <Svg {...p}><path d="M14.7 6.3a1 1 0 0 0 0 1.4l1.6 1.6a1 1 0 0 0 1.4 0l3.77-3.77a6 6 0 0 1-7.94 7.94l-6.91 6.91a2.12 2.12 0 0 1-3-3l6.91-6.91a6 6 0 0 1 7.94-7.94l-3.76 3.76z" /></Svg>
);
export const IconSettings = (p: P) => (
  <Svg {...p}><path d="M12.22 2h-.44a2 2 0 0 0-2 2v.18a2 2 0 0 1-1 1.73l-.43.25a2 2 0 0 1-2 0l-.15-.08a2 2 0 0 0-2.73.73l-.22.38a2 2 0 0 0 .73 2.73l.15.1a2 2 0 0 1 1 1.72v.51a2 2 0 0 1-1 1.74l-.15.09a2 2 0 0 0-.73 2.73l.22.38a2 2 0 0 0 2.73.73l.15-.08a2 2 0 0 1 2 0l.43.25a2 2 0 0 1 1 1.73V20a2 2 0 0 0 2 2h.44a2 2 0 0 0 2-2v-.18a2 2 0 0 1 1-1.73l.43-.25a2 2 0 0 1 2 0l.15.08a2 2 0 0 0 2.73-.73l.22-.39a2 2 0 0 0-.73-2.73l-.15-.08a2 2 0 0 1-1-1.74v-.5a2 2 0 0 1 1-1.74l.15-.09a2 2 0 0 0 .73-2.73l-.22-.38a2 2 0 0 0-2.73-.73l-.15.08a2 2 0 0 1-2 0l-.43-.25a2 2 0 0 1-1-1.73V4a2 2 0 0 0-2-2z" /><circle cx="12" cy="12" r="3" /></Svg>
);
export const IconSearch = (p: P) => <Svg {...p}><circle cx="11" cy="11" r="7.5" /><path d="m20.5 20.5-4.2-4.2" /></Svg>;
export const IconFilter = (p: P) => <Svg {...p}><path d="M3 6h18" /><path d="M7 12h10" /><path d="M10 18h4" /></Svg>;
export const IconFolder = (p: P) => (
  <Svg {...p}><path d="M20 20a2 2 0 0 0 2-2V8a2 2 0 0 0-2-2h-7.9a2 2 0 0 1-1.69-.9L9.6 3.9A2 2 0 0 0 7.93 3H4a2 2 0 0 0-2 2v13a2 2 0 0 0 2 2Z" /></Svg>
);
export const IconCopy = (p: P) => <Svg {...p}><rect x="8" y="8" width="14" height="14" rx="2" /><path d="M4 16c-1.1 0-2-.9-2-2V4c0-1.1.9-2 2-2h10c1.1 0 2 .9 2 2" /></Svg>;
export const IconCheck = (p: P) => <Svg {...p}><path d="M20 6 9 17l-5-5" /></Svg>;
export const IconX = (p: P) => <Svg {...p}><path d="M18 6 6 18" /><path d="m6 6 12 12" /></Svg>;
export const IconPlus = (p: P) => <Svg {...p}><path d="M12 5v14" /><path d="M5 12h14" /></Svg>;
export const IconMinus = (p: P) => <Svg {...p}><path d="M5 12h14" /></Svg>;
export const IconChevronDown = (p: P) => <Svg {...p}><path d="m6 9 6 6 6-6" /></Svg>;
export const IconChevronUp = (p: P) => <Svg {...p}><path d="m18 15-6-6-6 6" /></Svg>;
// IconUser is Lucide's "user-round", used for the person's avatar in a thread.
export const IconUser = (p: P) => <Svg {...p}><circle cx="12" cy="8" r="4.5" /><path d="M20 21a8 8 0 0 0-16 0" /></Svg>;

// Logo is the infrapal mark and wordmark.
export function Logo() {
  return (
    <span className="logo">
      <img className="logo-mark" src={markUrl} alt="" />
      <span className="logo-word">infrapal</span>
    </span>
  );
}

export function YouAvatar() {
  return <span className="avatar you-av" aria-hidden="true"><IconUser size={18} /></span>;
}

// AgentAvatar is the reply's avatar: Claude's mark for Claude Code, and the
// first letter of the name for any other agent.
export function AgentAvatar({ name }: { name: string }) {
  if (isClaude(name)) return <span className="avatar claude-av" aria-hidden="true"><img src={claudeUrl} alt="" /></span>;
  return <span className="avatar agent-av" aria-hidden="true">{name.trim().charAt(0).toUpperCase() || "?"}</span>;
}
