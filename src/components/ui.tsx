import { useEffect, type ButtonHTMLAttributes, type ReactNode } from "react";
import { CheckCircle2, Info, Loader2, X, XCircle } from "lucide-react";
import { cx } from "../lib/format";
import { dismissToast, toastStore, useStore } from "../lib/store";

type Variant = "primary" | "secondary" | "ghost" | "danger" | "accent";
type Size = "sm" | "md";

export function Button({
  variant = "secondary",
  size = "md",
  loading,
  icon,
  className,
  children,
  disabled,
  ...rest
}: ButtonHTMLAttributes<HTMLButtonElement> & {
  variant?: Variant;
  size?: Size;
  loading?: boolean;
  icon?: ReactNode;
}) {
  return (
    <button
      {...rest}
      disabled={disabled || loading}
      className={cx(
        "inline-flex shrink-0 items-center justify-center gap-1.5 rounded-lg font-medium whitespace-nowrap transition-colors outline-none",
        "focus-visible:ring-2 focus-visible:ring-accent/40 disabled:cursor-not-allowed disabled:opacity-50",
        size === "sm" ? "h-7 px-2.5 text-xs" : "h-8 px-3 text-[13px]",
        variant === "primary" && "bg-primary text-primary-foreground hover:opacity-90",
        variant === "accent" && "bg-accent text-white hover:brightness-105",
        variant === "secondary" && "border border-border bg-card text-foreground hover:bg-muted",
        variant === "ghost" && "text-muted-foreground hover:bg-muted hover:text-foreground",
        variant === "danger" && "border border-danger/30 bg-danger/8 text-danger hover:bg-danger/15",
        className,
      )}
    >
      {loading ? <Loader2 className="size-3.5 animate-spin" /> : icon}
      {children}
    </button>
  );
}

export function IconButton({
  label,
  className,
  children,
  active,
  ...rest
}: ButtonHTMLAttributes<HTMLButtonElement> & { label: string; active?: boolean }) {
  return (
    <button
      {...rest}
      title={label}
      aria-label={label}
      className={cx(
        "inline-flex size-7 shrink-0 items-center justify-center rounded-md text-muted-foreground transition-colors",
        "hover:bg-muted hover:text-foreground disabled:pointer-events-none disabled:opacity-40",
        active && "bg-muted text-foreground",
        className,
      )}
    >
      {children}
    </button>
  );
}

export function Spinner({ className }: { className?: string }) {
  return <Loader2 className={cx("size-3.5 animate-spin text-muted-foreground", className)} />;
}

export function Badge({
  children,
  tone = "neutral",
  className,
}: {
  children: ReactNode;
  tone?: "neutral" | "accent" | "success" | "warning" | "danger" | "info";
  className?: string;
}) {
  return (
    <span
      className={cx(
        "inline-flex h-5 items-center gap-1 rounded-md px-1.5 text-[11px] font-medium whitespace-nowrap",
        tone === "neutral" && "bg-muted text-muted-foreground",
        tone === "accent" && "bg-accent-soft text-accent",
        tone === "success" && "bg-success/12 text-success",
        tone === "warning" && "bg-warning/12 text-warning",
        tone === "danger" && "bg-danger/12 text-danger",
        tone === "info" && "bg-info/12 text-info",
        className,
      )}
    >
      {children}
    </span>
  );
}

export function StatusDot({ tone, pulse }: { tone: "success" | "warning" | "muted" | "danger"; pulse?: boolean }) {
  return (
    <span
      className={cx(
        "inline-block size-2 shrink-0 rounded-full",
        tone === "success" && "bg-success",
        tone === "warning" && "bg-warning",
        tone === "danger" && "bg-danger",
        tone === "muted" && "bg-muted-foreground/50",
        pulse && "pulse-dot",
      )}
    />
  );
}

export function Dialog({
  open,
  onClose,
  title,
  description,
  children,
  footer,
  width = 520,
}: {
  open: boolean;
  onClose: () => void;
  title: ReactNode;
  description?: ReactNode;
  children: ReactNode;
  footer?: ReactNode;
  width?: number;
}) {
  useEffect(() => {
    if (!open) return;
    const onKey = (e: KeyboardEvent) => e.key === "Escape" && onClose();
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [open, onClose]);
  if (!open) return null;
  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/40 p-6 backdrop-blur-[2px]" onMouseDown={onClose}>
      <div
        className="fade-in flex max-h-full w-full flex-col overflow-hidden rounded-xl border border-border bg-popover shadow-2xl"
        style={{ maxWidth: width }}
        onMouseDown={(e) => e.stopPropagation()}
      >
        <div className="flex items-start justify-between gap-4 px-5 pt-4 pb-3">
          <div>
            <div className="text-[15px] font-semibold">{title}</div>
            {description && <div className="mt-1 text-xs text-muted-foreground">{description}</div>}
          </div>
          <IconButton label="关闭" onClick={onClose}>
            <X className="size-4" />
          </IconButton>
        </div>
        <div className="min-h-0 flex-1 overflow-auto px-5 pb-4">{children}</div>
        {footer && <div className="flex justify-end gap-2 border-t border-border bg-muted/40 px-5 py-3">{footer}</div>}
      </div>
    </div>
  );
}

export function Field({ label, hint, children }: { label: ReactNode; hint?: ReactNode; children: ReactNode }) {
  return (
    <label className="block">
      <div className="mb-1.5 text-xs font-medium text-foreground">{label}</div>
      {children}
      {hint && <div className="mt-1 text-[11px] text-muted-foreground">{hint}</div>}
    </label>
  );
}

export const inputClass =
  "h-8 w-full rounded-lg border border-input bg-background px-2.5 text-[13px] outline-none transition-colors placeholder:text-muted-foreground/70 focus:border-accent/60 focus:ring-2 focus:ring-accent/15";

export function Toggle({ checked, onChange, label }: { checked: boolean; onChange: (v: boolean) => void; label?: ReactNode }) {
  return (
    <button type="button" onClick={() => onChange(!checked)} className="inline-flex items-center gap-2 text-xs">
      <span
        className={cx(
          "relative inline-flex h-[18px] w-8 items-center rounded-full transition-colors",
          checked ? "bg-accent" : "bg-input",
        )}
      >
        <span
          className={cx(
            "absolute size-3.5 rounded-full bg-white shadow transition-transform",
            checked ? "translate-x-[16px]" : "translate-x-[2px]",
          )}
        />
      </span>
      {label}
    </button>
  );
}

export function Segmented<T extends string>({
  value,
  onChange,
  options,
}: {
  value: T;
  onChange: (v: T) => void;
  options: { value: T; label: ReactNode }[];
}) {
  return (
    <div className="inline-flex rounded-lg bg-muted p-0.5">
      {options.map((o) => (
        <button
          key={o.value}
          type="button"
          onClick={() => onChange(o.value)}
          className={cx(
            "h-6 rounded-md px-2.5 text-xs font-medium transition-colors",
            value === o.value ? "bg-card text-foreground shadow-sm" : "text-muted-foreground hover:text-foreground",
          )}
        >
          {o.label}
        </button>
      ))}
    </div>
  );
}

export function Empty({ icon, title, children }: { icon: ReactNode; title: ReactNode; children?: ReactNode }) {
  return (
    <div className="flex h-full flex-col items-center justify-center gap-3 p-8 text-center">
      <div className="flex size-11 items-center justify-center rounded-xl bg-muted text-muted-foreground">{icon}</div>
      <div className="text-sm font-medium">{title}</div>
      {children && <div className="max-w-sm text-xs leading-relaxed text-muted-foreground">{children}</div>}
    </div>
  );
}

export function Toasts() {
  const toasts = useStore(toastStore);
  return (
    <div className="pointer-events-none fixed right-4 bottom-4 z-[60] flex w-80 flex-col gap-2">
      {toasts.map((t) => (
        <div
          key={t.id}
          className="fade-in pointer-events-auto flex gap-2.5 rounded-xl border border-border bg-popover p-3 shadow-lg"
        >
          {t.kind === "success" && <CheckCircle2 className="mt-px size-4 shrink-0 text-success" />}
          {t.kind === "error" && <XCircle className="mt-px size-4 shrink-0 text-danger" />}
          {t.kind === "info" && <Info className="mt-px size-4 shrink-0 text-info" />}
          <div className="min-w-0 flex-1">
            <div className="text-[13px] font-medium">{t.title}</div>
            {t.detail && (
              <div className="selectable mt-0.5 line-clamp-6 text-xs break-words whitespace-pre-wrap text-muted-foreground">
                {t.detail}
              </div>
            )}
          </div>
          <button className="text-muted-foreground hover:text-foreground" onClick={() => dismissToast(t.id)}>
            <X className="size-3.5" />
          </button>
        </div>
      ))}
    </div>
  );
}

export function Card({ className, children }: { className?: string; children: ReactNode }) {
  return <div className={cx("rounded-xl border border-border bg-card", className)}>{children}</div>;
}
