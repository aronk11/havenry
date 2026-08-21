import CodeLine from "./CodeLine.jsx";
import "./StepRail.css";

/**
 * StepRail — a numbered sequence.
 *
 * Numbered because order genuinely matters here: no control plane, no token;
 * no token, no host. Where order carries no information this kit does not
 * number things — numbers on an unordered list are decoration pretending to
 * be structure.
 */
export default function StepRail({ steps }) {
  return (
    <ol className="steprail">
      {steps.map((s, i) => (
        <li className="steprail__item" key={s.title}>
          <span className="display steprail__num" aria-hidden="true">
            {i + 1}
          </span>
          <div className="steprail__body">
            <h3 className="steprail__title">{s.title}</h3>
            <p className="steprail__text">{s.body}</p>
            {s.command && <CodeLine>{s.command}</CodeLine>}
          </div>
        </li>
      ))}
    </ol>
  );
}
