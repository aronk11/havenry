import { Panel, Tag } from "../kit";
import "./Features.css";

/**
 * Deliberately not numbered: these three are independent, not a sequence.
 * Numbering them would imply an order that does not exist.
 */
const FEATURES = [
  {
    tilt: -0.8,
    tag: "no more ssh roulette",
    title: "Every host on one page",
    body: "Containers, images, health, restart counts and live logs across all your machines. Stop opening four terminals to answer one question.",
  },
  {
    tilt: 0.6,
    tag: "observe by default",
    title: "Drift you can see",
    body: "Git holds what should run. Havenry compares it against what actually runs, shows the difference in plain words, and then waits for you to decide.",
  },
  {
    tilt: -0.4,
    tag: "digest-pinned",
    title: "Updates with a way back",
    body: "Every update records the digest it replaced. If the health check fails afterwards, it rolls back on its own. No hoping.",
  },
];

export default function Features() {
  return (
    <section className="wrap section features">
      <h2 className="section__title">Three habits it saves you from</h2>
      <p className="section__lede">
        Nothing here is clever. It is the boring part of running your own
        machines, done once and done properly.
      </p>

      <div className="features__grid">
        {FEATURES.map((f) => (
          <Panel key={f.title} tilt={f.tilt}>
            <Tag tone="sun">{f.tag}</Tag>
            <h3 className="features__title">{f.title}</h3>
            <p className="features__body">{f.body}</p>
          </Panel>
        ))}
      </div>
    </section>
  );
}
