import ReactMarkdown from "react-markdown";
import remarkGfm from "remark-gfm";
import { useNavigate } from "react-router-dom";

interface Props {
  content: string;
}

export default function Markdown({ content }: Props) {
  const navigate = useNavigate();

  return (
    <div className="max-w-none">
      <ReactMarkdown
        remarkPlugins={[remarkGfm]}
        components={{
          h1: ({ children }) => (
            <h1 className="text-[#ff8700] text-[22px] font-semibold mt-10 mb-4 pb-2 border-b border-[#222]" style={{ fontFamily: "'JetBrains Mono', monospace" }}>
              {children}
            </h1>
          ),
          h2: ({ children }) => (
            <h2 className="text-[#e0e0e0] text-[17px] font-semibold mt-8 mb-3" style={{ fontFamily: "'JetBrains Mono', monospace" }}>
              {children}
            </h2>
          ),
          h3: ({ children }) => (
            <h3 className="text-[#c0c0c0] text-[15px] font-semibold mt-5 mb-2" style={{ fontFamily: "'JetBrains Mono', monospace" }}>
              {children}
            </h3>
          ),
          p: ({ children }) => (
            <p className="text-[#b0b0b0] leading-[1.7] mb-4 text-[15px]">{children}</p>
          ),
          a: ({ href, children }) => {
            const isInternal = href?.startsWith("/homelab-planner/");
            if (isInternal) {
              return (
                <a
                  href={href}
                  onClick={(e) => {
                    e.preventDefault();
                    const path = href!.replace("/homelab-planner", "");
                    navigate(path);
                  }}
                  className="text-[#00ff00] hover:text-[#33ff33] underline underline-offset-2 decoration-[#00ff00]/30 hover:decoration-[#00ff00]/60 transition-colors"
                >
                  {children}
                </a>
              );
            }
            return (
              <a
                href={href}
                target="_blank"
                rel="noopener noreferrer"
                className="text-[#00ff00] hover:text-[#33ff33] underline underline-offset-2 decoration-[#00ff00]/30 hover:decoration-[#00ff00]/60 transition-colors"
              >
                {children}
              </a>
            );
          },
          ul: ({ children }) => (
            <ul className="mb-4 text-[#b0b0b0] space-y-1.5 pl-6 list-disc marker:text-[#555]">
              {children}
            </ul>
          ),
          ol: ({ children }) => (
            <ol className="mb-4 text-[#b0b0b0] space-y-1.5 pl-6 list-decimal marker:text-[#555]">
              {children}
            </ol>
          ),
          li: ({ children }) => (
            <li className="text-[#b0b0b0] text-[15px] leading-[1.7] pl-1">{children}</li>
          ),
          strong: ({ children }) => (
            <strong className="text-[#e0e0e0] font-semibold">{children}</strong>
          ),
          em: ({ children }) => (
            <em className="text-[#999] italic">{children}</em>
          ),
          code: ({ children, className }) => {
            const isBlock = className?.includes("language-");
            if (isBlock) {
              return (
                <code className="block bg-[#111] border border-[#222] rounded-md p-4 text-[13px] text-[#c0c0c0] overflow-x-auto mb-4" style={{ fontFamily: "'JetBrains Mono', monospace" }}>
                  {children}
                </code>
              );
            }
            return (
              <code className="bg-[#1a1a1a] border border-[#2a2a2a] rounded px-1.5 py-0.5 text-[13px] text-[#e0e0e0]" style={{ fontFamily: "'JetBrains Mono', monospace" }}>
                {children}
              </code>
            );
          },
          pre: ({ children }) => <pre className="mb-4">{children}</pre>,
          blockquote: ({ children }) => (
            <blockquote className="border-l-2 border-[#ff8700]/50 pl-4 my-5 text-[#999]">
              {children}
            </blockquote>
          ),
          table: ({ children }) => (
            <div className="overflow-x-auto mb-6 rounded-md border border-[#222]">
              <table className="w-full border-collapse text-[14px]">{children}</table>
            </div>
          ),
          thead: ({ children }) => (
            <thead className="bg-[#151515]">{children}</thead>
          ),
          th: ({ children }) => (
            <th className="text-left px-4 py-2.5 text-[#999] text-[12px] font-medium border-b border-[#222]" style={{ fontFamily: "'JetBrains Mono', monospace", letterSpacing: '0.03em' }}>
              {children}
            </th>
          ),
          tr: ({ children }) => (
            <tr className="border-b border-[#191919] hover:bg-[#151515] transition-colors">
              {children}
            </tr>
          ),
          td: ({ children }) => (
            <td className="px-4 py-2.5 text-[#b0b0b0]">
              {children}
            </td>
          ),
          hr: () => <hr className="border-[#222] my-8" />,
        }}
      >
        {content}
      </ReactMarkdown>
    </div>
  );
}
