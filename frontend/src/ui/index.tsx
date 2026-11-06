// Kloudia's own small component set. Components are thin wrappers over native elements styled by ui.css.
import { AlertCircle, CheckCircle2, Info, Loader2, TriangleAlert, X } from "lucide-react";
import type { LucideIcon } from "lucide-react";
import type { ButtonHTMLAttributes, InputHTMLAttributes, ReactNode, SelectHTMLAttributes, TextareaHTMLAttributes } from "react";
import { createContext, useCallback, useContext, useEffect, useId, useRef, useState } from "react";
import { Link } from "react-router-dom";
import "./ui.css";

export type Tone = "neutral" | "success" | "warning" | "error" | "info";

type ButtonProps = ButtonHTMLAttributes<HTMLButtonElement> & {
  variant?: "primary" | "secondary" | "ghost" | "danger";
  size?: "sm" | "md";
  icon?: LucideIcon;
  loading?: boolean;
};

export function Button({ variant = "secondary", size = "md", icon: Icon, loading, children, className, disabled, ...rest }: ButtonProps) {
  return (
    <button
      type="button"
      className={`btn btn-${variant} btn-${size}${className ? ` ${className}` : ""}`}
      disabled={disabled || loading}
      {...rest}
    >
      {loading ? <Loader2 className="spin" size={16} aria-hidden /> : Icon && <Icon size={16} aria-hidden />}
      {children}
    </button>
  );
}

export function IconButton({
  label,
  icon: Icon,
  variant = "ghost",
  ...rest
}: Omit<ButtonHTMLAttributes<HTMLButtonElement>, "children"> & { label: string; icon: LucideIcon; variant?: "ghost" | "danger" }) {
  return (
    <button type="button" className={`icon-btn icon-btn-${variant}`} aria-label={label} title={label} {...rest}>
      <Icon size={16} aria-hidden />
    </button>
  );
}

export function Badge({ tone = "neutral", dot, children, title }: { tone?: Tone; dot?: boolean; children: ReactNode; title?: string }) {
  return (
    <span className={`badge badge-${tone}`} title={title}>
      {dot && <span className="badge-dot" aria-hidden />}
      {children}
    </span>
  );
}

export function Spinner({ label }: { label?: string }) {
  return (
    <span className="spinner" role="status">
      <Loader2 className="spin" size={16} aria-hidden />
      {label}
    </span>
  );
}

export function Card({
  title,
  description,
  actions,
  children,
  flush,
  className,
}: {
  title?: ReactNode;
  description?: ReactNode;
  actions?: ReactNode;
  children?: ReactNode;
  flush?: boolean; // no body padding, for tables that run edge to edge
  className?: string;
}) {
  return (
    <section className={`card${className ? ` ${className}` : ""}`}>
      {(title || actions) && (
        <header className="card-header">
          <div>
            {title && <h2 className="card-title">{title}</h2>}
            {description && <p className="card-description">{description}</p>}
          </div>
          {actions && <div className="card-actions">{actions}</div>}
        </header>
      )}
      <div className={flush ? "card-body card-body-flush" : "card-body"}>{children}</div>
    </section>
  );
}

export function PageHeader({
  title,
  subtitle,
  badge,
  actions,
  crumbs,
}: {
  title: ReactNode;
  subtitle?: ReactNode;
  badge?: ReactNode;
  actions?: ReactNode;
  crumbs?: Array<{ label: string; to?: string }>;
}) {
  return (
    <div className="page-header">
      {crumbs && crumbs.length > 0 && (
        <nav className="crumbs" aria-label="breadcrumb">
          {crumbs.map((crumb, index) => (
            <span key={`${crumb.label}-${index}`}>
              {crumb.to ? <Link to={crumb.to}>{crumb.label}</Link> : <span aria-current="page">{crumb.label}</span>}
            </span>
          ))}
        </nav>
      )}
      <div className="page-header-row">
        <div className="page-header-text">
          <div className="page-title-row">
            <h1>{title}</h1>
            {badge}
          </div>
          {subtitle && <p className="page-subtitle">{subtitle}</p>}
        </div>
        {actions && <div className="page-actions">{actions}</div>}
      </div>
    </div>
  );
}

const alertIcons: Record<Tone, LucideIcon> = {
  neutral: Info,
  info: Info,
  success: CheckCircle2,
  warning: TriangleAlert,
  error: AlertCircle,
};

export function Alert({ tone = "info", title, children, action }: { tone?: Tone; title?: ReactNode; children?: ReactNode; action?: ReactNode }) {
  const Icon = alertIcons[tone];
  return (
    <div className={`alert alert-${tone}`} role={tone === "error" ? "alert" : "status"}>
      <Icon size={18} aria-hidden className="alert-icon" />
      <div className="alert-body">
        {title && <strong>{title}</strong>}
        {children && <div>{children}</div>}
      </div>
      {action}
    </div>
  );
}

export function Field({
  label,
  hint,
  error,
  children,
  className,
}: {
  label: ReactNode;
  hint?: ReactNode;
  error?: ReactNode;
  children: (id: string) => ReactNode;
  className?: string;
}) {
  const id = useId();
  return (
    <div className={`field${className ? ` ${className}` : ""}`}>
      <label htmlFor={id}>{label}</label>
      {children(id)}
      {error ? <span className="field-error">{error}</span> : hint && <span className="field-hint">{hint}</span>}
    </div>
  );
}

export function Input(props: InputHTMLAttributes<HTMLInputElement>) {
  return <input className="input" {...props} />;
}

export function Select(props: SelectHTMLAttributes<HTMLSelectElement>) {
  return <select className="input select" {...props} />;
}

export function Textarea(props: TextareaHTMLAttributes<HTMLTextAreaElement>) {
  return <textarea className="input textarea" {...props} />;
}

export function Switch({ checked, onChange, label }: { checked: boolean; onChange: (value: boolean) => void; label: string }) {
  return (
    <label className="switch">
      <input type="checkbox" role="switch" checked={checked} onChange={(event) => onChange(event.target.checked)} />
      <span className="switch-track" aria-hidden>
        <span className="switch-thumb" />
      </span>
      <span>{label}</span>
    </label>
  );
}

export function DescriptionList({ items }: { items: Array<{ label: ReactNode; value: ReactNode }> }) {
  return (
    <dl className="dl">
      {items.map((item, index) => (
        <div className="dl-row" key={index}>
          <dt>{item.label}</dt>
          <dd>{item.value === "" || item.value === undefined || item.value === null ? "—" : item.value}</dd>
        </div>
      ))}
    </dl>
  );
}

export function Empty({ icon: Icon, title, children }: { icon?: LucideIcon; title: ReactNode; children?: ReactNode }) {
  return (
    <div className="empty">
      {Icon && <Icon size={28} aria-hidden />}
      <strong>{title}</strong>
      {children && <div className="empty-body">{children}</div>}
    </div>
  );
}

export function Tabs<T extends string>({
  tabs,
  value,
  onChange,
  label,
}: {
  tabs: Array<{ value: T; label: string }>;
  value: T;
  onChange: (value: T) => void;
  label: string;
}) {
  return (
    <div className="tabs" role="tablist" aria-label={label}>
      {tabs.map((tab) => (
        <button
          key={tab.value}
          type="button"
          role="tab"
          aria-selected={tab.value === value}
          className={tab.value === value ? "tab tab-active" : "tab"}
          onClick={() => onChange(tab.value)}
        >
          {tab.label}
        </button>
      ))}
    </div>
  );
}

export function Modal({
  title,
  onClose,
  children,
  footer,
}: {
  title: ReactNode;
  onClose: () => void;
  children: ReactNode;
  footer?: ReactNode;
}) {
  const ref = useRef<HTMLDivElement>(null);
  const titleId = useId();
  useEffect(() => {
    const previous = document.activeElement as HTMLElement | null;
    const first = ref.current?.querySelector<HTMLElement>("input, select, textarea, button:not(.modal-close)");
    (first ?? ref.current)?.focus();
    const onKey = (event: globalThis.KeyboardEvent) => {
      if (event.key === "Escape") {
        onClose();
      }
    };
    window.addEventListener("keydown", onKey);
    return () => {
      window.removeEventListener("keydown", onKey);
      previous?.focus();
    };
    // Focus once when the dialog opens.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);
  return (
    <div className="modal-backdrop" onMouseDown={(event) => event.target === event.currentTarget && onClose()}>
      <div className="modal" role="dialog" aria-modal="true" aria-labelledby={titleId} ref={ref} tabIndex={-1}>
        <header className="modal-header">
          <h2 id={titleId}>{title}</h2>
          <button type="button" className="icon-btn icon-btn-ghost modal-close" aria-label="close" onClick={onClose}>
            <X size={16} aria-hidden />
          </button>
        </header>
        <div className="modal-body">{children}</div>
        {footer && <footer className="modal-footer">{footer}</footer>}
      </div>
    </div>
  );
}

type ConfirmRequest = {
  title: string;
  message: ReactNode;
  confirmLabel: string;
  cancelLabel: string;
  danger?: boolean;
};

const ConfirmContext = createContext<(request: ConfirmRequest) => Promise<boolean>>(async () => false);

// ConfirmProvider lets any page ask "are you sure?" with `await confirm({...})`.
export function ConfirmProvider({ children }: { children: ReactNode }) {
  const [pending, setPending] = useState<(ConfirmRequest & { resolve: (ok: boolean) => void }) | null>(null);
  const confirm = useCallback(
    (request: ConfirmRequest) => new Promise<boolean>((resolve) => setPending({ ...request, resolve })),
    [],
  );
  const close = (ok: boolean) => {
    pending?.resolve(ok);
    setPending(null);
  };
  return (
    <ConfirmContext.Provider value={confirm}>
      {children}
      {pending && (
        <Modal
          title={pending.title}
          onClose={() => close(false)}
          footer={
            <>
              <Button onClick={() => close(false)}>{pending.cancelLabel}</Button>
              <Button variant={pending.danger ? "danger" : "primary"} onClick={() => close(true)}>
                {pending.confirmLabel}
              </Button>
            </>
          }
        >
          {pending.message}
        </Modal>
      )}
    </ConfirmContext.Provider>
  );
}

export function useConfirm() {
  return useContext(ConfirmContext);
}
