import { useId } from "react";

/** The app's mark, drawn as resources/icon.svg draws the app icon. */
export function AppMark({ size = 28 }: { size?: number }) {
  const id = useId();
  return (
    <svg width={size} height={size} viewBox="80 80 864 864" aria-hidden="true" className="shrink-0">
      <defs>
        <linearGradient id={`${id}bg`} x1="0" y1="0" x2="0" y2="1">
          <stop offset="0" stopColor="#2a2a2f" />
          <stop offset="1" stopColor="#141417" />
        </linearGradient>
        <linearGradient id={`${id}ring`} x1="0.15" y1="0.1" x2="0.85" y2="0.95">
          <stop offset="0" stopColor="#ffc42e" />
          <stop offset="1" stopColor="#f26b0f" />
        </linearGradient>
      </defs>
      <rect x="80" y="80" width="864" height="864" rx="196" fill={`url(#${id}bg)`} />
      <path d="M 709.1 689.4 A 265 265 0 1 1 709.1 334.6" fill="none" stroke={`url(#${id}ring)`} strokeWidth="104" strokeLinecap="round" />
      <polyline points="414,426 500,512 414,598" fill="none" stroke="#ffffff" strokeWidth="54" strokeLinecap="round" strokeLinejoin="round" />
      <line x1="534" y1="598" x2="626" y2="598" stroke="#ffb020" strokeWidth="54" strokeLinecap="round" />
    </svg>
  );
}
