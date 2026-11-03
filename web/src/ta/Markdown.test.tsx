import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { Markdown } from "./Markdown";

describe("Markdown", () => {
  it("renders bold, inline code, lists and paragraphs", () => {
    const { container } = render(
      <Markdown text={"**What I did**\n- **Backup:** saved to `~/old`\n- Built it\n\nNext:\n1. Restart\n2. Check"} />,
    );
    expect(container.querySelector("strong")?.textContent).toBe("What I did");
    expect(container.querySelectorAll("ul li")).toHaveLength(2);
    expect(container.querySelector("ul li strong")?.textContent).toBe("Backup:");
    expect(container.querySelector("code")?.textContent).toBe("~/old");
    expect(container.querySelectorAll("ol li")).toHaveLength(2);
    expect(screen.getByText("Next:")).toBeTruthy();
    expect(container.textContent).not.toContain("**");
  });

  it("keeps line breaks inside a paragraph and renders fenced code as written", () => {
    const { container } = render(<Markdown text={"one\ntwo\n\n```sh\nls -la\n  cd x\n```"} />);
    expect(container.querySelectorAll("p br")).toHaveLength(1);
    expect(container.querySelector("pre")?.textContent).toBe("ls -la\n  cd x");
  });

  it("never turns text into markup", () => {
    const { container } = render(<Markdown text={'<img src=x onerror="alert(1)"> **ok**'} />);
    expect(container.querySelector("img")).toBeNull();
    expect(container.textContent).toContain("<img");
  });

  it("leaves an unclosed marker as plain text", () => {
    const { container } = render(<Markdown text="a **b and `c" />);
    expect(container.textContent).toBe("a **b and `c");
  });
});
