import type { ReactNode } from "react";

// Markdown shows the small part of markdown an assistant's reply uses:
// paragraphs, bullet and numbered lists, headings, fenced and inline code, and
// bold. It builds React elements rather than HTML, so nothing in the text can
// run as markup.

const INLINE = /(`[^`\n]+`|\*\*[^*\n]+\*\*)/g;

function inline(text: string): ReactNode[] {
  return text.split(INLINE).map((part, i) => {
    if (part.startsWith("`") && part.endsWith("`") && part.length > 2) return <code key={i}>{part.slice(1, -1)}</code>;
    if (part.startsWith("**") && part.endsWith("**") && part.length > 4) return <strong key={i}>{part.slice(2, -2)}</strong>;
    return part;
  });
}

const BULLET = /^\s*[-*•]\s+(.*)$/;
const NUMBER = /^\s*\d+[.)]\s+(.*)$/;
const HEADING = /^#{1,6}\s+(.*)$/;

export function Markdown({ text }: { text: string }) {
  const lines = text.replace(/\r\n/g, "\n").split("\n");
  const out: ReactNode[] = [];
  let i = 0;
  let key = 0;
  while (i < lines.length) {
    const line = lines[i];
    if (line.trim() === "") {
      i++;
      continue;
    }
    if (line.trim().startsWith("```")) {
      const code: string[] = [];
      i++;
      while (i < lines.length && !lines[i].trim().startsWith("```")) code.push(lines[i++]);
      i++; // the closing fence, if there is one
      out.push(<pre className="md-code" key={key++}>{code.join("\n")}</pre>);
      continue;
    }
    const heading = HEADING.exec(line);
    if (heading) {
      out.push(<p className="md-h" key={key++}><strong>{inline(heading[1])}</strong></p>);
      i++;
      continue;
    }
    const kind = BULLET.test(line) ? "ul" : NUMBER.test(line) ? "ol" : null;
    if (kind) {
      const re = kind === "ul" ? BULLET : NUMBER;
      const items: ReactNode[] = [];
      while (i < lines.length && re.test(lines[i])) {
        items.push(<li key={items.length}>{inline(re.exec(lines[i])![1])}</li>);
        i++;
      }
      out.push(kind === "ul" ? <ul key={key++}>{items}</ul> : <ol key={key++}>{items}</ol>);
      continue;
    }
    // A paragraph runs until a blank line or the start of another block.
    const para: string[] = [];
    while (
      i < lines.length &&
      lines[i].trim() !== "" &&
      !lines[i].trim().startsWith("```") &&
      !HEADING.test(lines[i]) &&
      !BULLET.test(lines[i]) &&
      !NUMBER.test(lines[i])
    ) {
      para.push(lines[i++]);
    }
    out.push(
      <p key={key++}>
        {para.map((l, n) => (
          <span key={n}>
            {n > 0 && <br />}
            {inline(l)}
          </span>
        ))}
      </p>,
    );
  }
  return <div className="md">{out}</div>;
}
