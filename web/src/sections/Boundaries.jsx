import "./Boundaries.css";

/**
 * The unusual section on a landing page: what the product refuses to do.
 *
 * It earns its place here because this audience distrusts tools that promise
 * everything, and because each line is a real recorded decision rather than a
 * gap waiting to be filled.
 */
const LIMITS = [
  ["Not Kubernetes.", "Docker and Compose only. Your compose files stay exactly as you wrote them."],
  ["Not a tunnel.", "Tailscale and Cloudflare already do that well. Havenry integrates with them instead of competing."],
  ["Not a shell.", "No SSH through the web UI. A shell would undo every permission check above it."],
  ["Not phoning home.", "No telemetry. Not anonymous, not opt-out. None."],
];

export default function Boundaries() {
  return (
    <section className="boundaries">
      <div className="wrap boundaries__grid">
        <div>
          <h2 className="boundaries__title">What it is not</h2>
          <p className="boundaries__lede">
            Knowing where a tool stops is worth as much as knowing what it does.
            These are decisions, not gaps.
          </p>
        </div>
        <ul className="boundaries__list">
          {LIMITS.map(([lead, rest]) => (
            <li key={lead}>
              <span className="boundaries__marker" aria-hidden="true" />
              <span>
                <strong>{lead}</strong> <span className="boundaries__rest">{rest}</span>
              </span>
            </li>
          ))}
        </ul>
      </div>
    </section>
  );
}
