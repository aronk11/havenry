import { Cloud, CodeLine, HostCloud, Panel, PuffButton, Sky, StepRail, Tag } from "../kit";
import "./KitShowcase.css";

/**
 * The kit, shown as itself.
 *
 * A design system that only exists inside a page is a set of one-offs. Putting
 * the pieces on the page they built is the cheapest honest proof that they are
 * reusable.
 */
const PIECES = [
  {
    name: "Cloud",
    note: "The base shape. One SVG path, so the outline never runs through the middle.",
    demo: <Cloud width={128} />,
  },
  {
    name: "PuffButton",
    note: "Sinks by exactly its shadow offset when pressed.",
    demo: <PuffButton>Press me</PuffButton>,
  },
  {
    name: "Tag",
    note: "Status pill. Colour never carries the meaning alone.",
    demo: (
      <div className="kit__row">
        <Tag tone="good">in sync</Tag>
        <Tag tone="warn">1 drift</Tag>
        <Tag tone="sun">observe</Tag>
      </div>
    ),
  },
  {
    name: "HostCloud",
    note: "A host, drawn as a cloud. The squares inside are its containers.",
    demo: <HostCloud name="nas-01" width={132} services={[{ ok: true }, { ok: false }]} />,
  },
  {
    name: "CodeLine",
    // Breiter als die anderen Kacheln: Eine Komponente, die eine Zeile Text
    // und einen Knopf nebeneinander zeigt, braucht Platz. Sie in eine schmale
    // Kachel zu zwängen und den Befehl abzuschneiden ließe die Komponente
    // kaputt aussehen, obwohl sie korrekt scrollt.
    wide: true,
    note: "One command, one copy button that resets itself.",
    demo: <CodeLine>havenry status --host nas-01</CodeLine>,
  },
  {
    name: "Panel",
    note: "For content that needs corners. Optional slight tilt.",
    demo: <Panel tilt={-1.4} className="kit__panelDemo">Tilted 1.4°</Panel>,
  },
  {
    name: "StepRail",
    note: "A numbered sequence — used only where order actually matters.",
    demo: (
      <div className="kit__railDemo">
        <StepRail steps={[{ title: "First move", body: "Then the next one." }]} />
      </div>
    ),
  },
  {
    name: "Sky",
    note: "The backdrop. Owns the only gradient in the whole system.",
    demo: (
      <Sky>
        <div className="kit__skyDemo" />
      </Sky>
    ),
  },
];

export default function KitShowcase() {
  return (
    <section id="kit" className="kit">
      <div className="wrap">
        <h2 className="section__title">The kit behind this page</h2>
        <p className="section__lede">
          Eight components, one rule: thick outline, hard offset shadow, no
          gradients on surfaces. The gradient lives in the sky and nowhere else.
        </p>

        <div className="kit__grid">
          {PIECES.map((p) => (
            <article className={`kit__card ${p.wide ? "kit__card--wide" : ""}`} key={p.name}>
              <div className="kit__stage">{p.demo}</div>
              <div>
                <h3 className="mono kit__name">&lt;{p.name} /&gt;</h3>
                <p className="kit__note">{p.note}</p>
              </div>
            </article>
          ))}
        </div>
      </div>
    </section>
  );
}
