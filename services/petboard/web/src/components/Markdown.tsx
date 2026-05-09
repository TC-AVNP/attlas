import { useEffect, useRef, useId } from "react";
import ReactMarkdown from "react-markdown";
import remarkGfm from "remark-gfm";
import mermaid from "mermaid";

mermaid.initialize({
  startOnLoad: false,
  theme: "dark",
  themeVariables: {
    darkMode: true,
    background: "#0a0a0a",
    primaryColor: "#3b82f6",
    primaryTextColor: "#e5e5e5",
    primaryBorderColor: "#404040",
    lineColor: "#525252",
    secondaryColor: "#1e3a5f",
    tertiaryColor: "#1a1a2e",
    noteBkgColor: "#1e1e1e",
    noteTextColor: "#d4d4d4",
    fontFamily: "ui-monospace, monospace",
  },
});

function MermaidBlock({ code }: { code: string }) {
  const ref = useRef<HTMLDivElement>(null);
  const id = useId().replace(/:/g, "m");

  useEffect(() => {
    if (!ref.current) return;
    ref.current.innerHTML = "";
    mermaid
      .render(`mermaid-${id}`, code)
      .then(({ svg }) => {
        if (ref.current) ref.current.innerHTML = svg;
      })
      .catch(() => {
        if (ref.current)
          ref.current.innerHTML = `<pre class="text-red-400 text-xs">${code}</pre>`;
      });
  }, [code, id]);

  return (
    <div
      ref={ref}
      className="my-4 flex justify-center overflow-x-auto rounded border border-neutral-800 bg-neutral-950 p-4"
    />
  );
}

export default function Markdown({ content }: { content: string }) {
  return (
    <div className="prose prose-invert prose-sm max-w-none
      prose-headings:text-neutral-200 prose-headings:font-semibold prose-headings:tracking-tight
      prose-h2:text-base prose-h2:mt-6 prose-h2:mb-2 prose-h2:border-b prose-h2:border-neutral-800 prose-h2:pb-1
      prose-h3:text-sm prose-h3:mt-4 prose-h3:mb-1
      prose-p:text-neutral-400 prose-p:leading-relaxed
      prose-li:text-neutral-400 prose-li:leading-relaxed
      prose-strong:text-neutral-200
      prose-code:text-amber-400 prose-code:bg-neutral-900 prose-code:px-1 prose-code:py-0.5 prose-code:rounded prose-code:text-xs
      prose-pre:bg-neutral-900 prose-pre:border prose-pre:border-neutral-800
      prose-a:text-blue-400 prose-a:no-underline hover:prose-a:underline
      prose-hr:border-neutral-800
      prose-table:text-sm
      prose-th:text-neutral-300 prose-th:border-neutral-700
      prose-td:text-neutral-400 prose-td:border-neutral-800"
    >
      <ReactMarkdown
        remarkPlugins={[remarkGfm]}
        components={{
          code({ className, children, ...props }) {
            const match = /language-mermaid/.exec(className || "");
            if (match) {
              return <MermaidBlock code={String(children).trim()} />;
            }
            return (
              <code className={className} {...props}>
                {children}
              </code>
            );
          },
        }}
      >
        {content}
      </ReactMarkdown>
    </div>
  );
}
