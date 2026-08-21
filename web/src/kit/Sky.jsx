import Cloud from "./Cloud.jsx";
import "./Sky.css";

/**
 * Sky — the page backdrop.
 *
 * The only gradient in the entire kit lives here. Surfaces stay flat; once
 * cards start carrying gradients the sticker look loses its footing.
 *
 * Three ambient clouds, heavily dimmed. More motion would pull attention away
 * from the interactive part below, which is the thing worth looking at.
 */
const AMBIENT = [
  { top: "8%", size: 320, duration: 150, delay: 0, opacity: 0.13 },
  { top: "34%", size: 210, duration: 195, delay: -60, opacity: 0.1 },
  { top: "62%", size: 400, duration: 240, delay: -130, opacity: 0.09 },
];

export default function Sky({ children }) {
  return (
    <div className="sky">
      <div className="sky__ambient" aria-hidden="true">
        {AMBIENT.map((p, i) => (
          <div
            key={i}
            className="sky__puff"
            style={{
              top: p.top,
              opacity: p.opacity,
              animationDuration: `${p.duration}s`,
              animationDelay: `${p.delay}s`,
            }}
          >
            <Cloud width={p.size} outline="transparent" />
          </div>
        ))}
      </div>
      <div className="sky__content">{children}</div>
    </div>
  );
}
