import { useEffect, useRef, useState } from "react";
import "./CodeLine.css";

/**
 * CodeLine — one command, one copy button.
 *
 * The label resets after two seconds. A button that keeps claiming "Copied"
 * is lying the second time someone looks at it.
 *
 * Copying can fail — clipboard access needs a secure context and permission.
 * When it does, the button says so rather than pretending it worked.
 */
export default function CodeLine({ children, prompt = "$" }) {
  const [state, setState] = useState("idle"); // idle | copied | failed
  const timer = useRef(null);

  useEffect(() => () => clearTimeout(timer.current), []);

  const copy = async () => {
    clearTimeout(timer.current);
    try {
      await navigator.clipboard.writeText(String(children));
      setState("copied");
    } catch {
      setState("failed");
    }
    timer.current = setTimeout(() => setState("idle"), 2000);
  };

  const label = { idle: "Copy", copied: "Copied", failed: "Select it" }[state];

  return (
    <div className="codeline">
      <code className="mono codeline__text">
        <span className="codeline__prompt">{prompt} </span>
        {children}
      </code>
      <button
        type="button"
        onClick={copy}
        className={`codeline__btn codeline__btn--${state}`}
        aria-label={`Copy command: ${children}`}
      >
        {label}
      </button>
      <span role="status" aria-live="polite" className="codeline__sr">
        {state === "copied" ? "Command copied" : ""}
      </span>
    </div>
  );
}
