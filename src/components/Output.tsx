import { memo, useMemo } from "react";
import { AnsiUp } from "ansi_up";
import type { Chunk } from "../lib/api";
import { cx } from "../lib/format";

const ansi = new AnsiUp();
ansi.use_classes = false;
ansi.escape_html = true;

/** Applies carriage returns as a terminal would: a line keeps what came last. */
function applyCR(text: string): string {
  if (!text.includes("\r")) return text;
  return text
    .replace(/\r\n/g, "\n")
    .split("\n")
    .map((line) => {
      const i = line.lastIndexOf("\r");
      return i >= 0 ? line.slice(i + 1) : line;
    })
    .join("\n");
}

const TextBlock = memo(function TextBlock({ text, stream }: { text: string; stream: string }) {
  const html = useMemo(() => ansi.ansi_to_html(applyCR(text)), [text]);
  return (
    <div
      className={cx(
        "ansi-out selectable",
        stream === "stderr" && "text-[color-mix(in_srgb,var(--danger)_75%,var(--foreground))]",
        stream === "info" && "text-muted-foreground",
      )}
      dangerouslySetInnerHTML={{ __html: html }}
    />
  );
});

export function Output({ chunks, hideInfo }: { chunks: Chunk[]; hideInfo?: boolean }) {
  return (
    <div className="flex flex-col gap-1">
      {chunks.map((c, i) => {
        if (c.stream === "image") {
          return (
            <img
              key={i}
              src={c.image}
              title={c.path}
              className="my-1 max-h-[480px] max-w-full self-start rounded-lg border border-border bg-white"
            />
          );
        }
        if (hideInfo && c.stream === "info") return null;
        if (!c.text) return null;
        return <TextBlock key={i} text={c.text} stream={c.stream} />;
      })}
    </div>
  );
}
