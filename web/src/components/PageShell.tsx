import type { ReactNode } from "react";

export function PageShell({
  title,
  subtitle,
  children,
}: {
  title: string;
  subtitle?: string;
  children?: ReactNode;
}) {
  return (
    <div className="mx-auto flex max-w-[1400px] flex-col gap-5 px-8 py-7 pb-14">
      <div>
        <h1 className="font-display text-[29px] font-bold tracking-tight text-ink">
          {title}
        </h1>
        {subtitle && <p className="mt-1.5 text-sm text-ink/55">{subtitle}</p>}
      </div>
      {children}
    </div>
  );
}
