import { NavLink } from "react-router-dom";
import type { ReactNode } from "react";
import type { Theme } from "../lib/theme";

type NavItem = {
  to: string;
  label: string;
  icon: ReactNode;
};

const iconProps = {
  width: 17,
  height: 17,
  viewBox: "0 0 24 24",
  fill: "none",
  stroke: "currentColor",
  strokeWidth: 1.8,
  strokeLinecap: "round" as const,
  strokeLinejoin: "round" as const,
};

const items: NavItem[] = [
  {
    to: "/",
    label: "Fix",
    icon: (
      <svg {...iconProps}>
        <path d="M13 2 3 14h7l-1 8 10-12h-7l1-8z" />
      </svg>
    ),
  },
  {
    to: "/spend",
    label: "Spend",
    icon: (
      <svg {...iconProps}>
        <path d="M12 2v20M17 5H9.5a3.5 3.5 0 0 0 0 7h5a3.5 3.5 0 0 1 0 7H7" />
      </svg>
    ),
  },
  {
    to: "/trace",
    label: "Trace",
    icon: (
      <svg {...iconProps}>
        <path d="M3 12h4l3-7 4 14 3-7h4" />
      </svg>
    ),
  },
];

export function NavRail({
  theme,
  onToggleTheme,
}: {
  theme: Theme;
  onToggleTheme: () => void;
}) {
  return (
    <nav className="flex w-[236px] shrink-0 flex-col border-r border-wire bg-chrome p-3">
      <div className="px-2 pb-2.5 text-[11px] tracking-wide text-ink/35 uppercase">
        Dashboard
      </div>
      <div className="flex flex-col gap-0.5">
        {items.map((item) => (
          <NavLink
            key={item.to}
            to={item.to}
            end={item.to === "/"}
            className={({ isActive }) =>
              [
                "flex items-center gap-2.5 rounded-md px-2.5 py-2 text-[13.5px] font-medium transition-colors",
                isActive
                  ? "bg-signal/15 text-ink shadow-[inset_2px_0_0_var(--color-signal)]"
                  : "text-ink/55 hover:bg-panel-2 hover:text-ink",
              ].join(" ")
            }
          >
            {item.icon}
            {item.label}
          </NavLink>
        ))}
      </div>
      <button
        type="button"
        onClick={onToggleTheme}
        className="mt-auto flex items-center gap-2.5 border-t border-wire px-2 pt-3 text-xs text-ink/45 hover:text-ink"
      >
        <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round">
          <circle cx="12" cy="12" r="4" />
          <path d="M12 3v2M12 19v2M5 12H3M21 12h-2" />
        </svg>
        {theme === "dark" ? "Light theme" : "Dark theme"}
      </button>
    </nav>
  );
}
