import { Panel, StepRail } from "../kit";
import "./Setup.css";

const STEPS = [
  {
    title: "Start the control plane",
    body: "One container. It generates its own TLS certificate and prints an admin password exactly once — take it from the logs.",
    command: "docker compose -f deploy/compose.yaml up -d havenry",
  },
  {
    title: "Connect your hosts",
    body: "Create a token in the UI, then run the agent on each machine. Every host has to be approved by you before it will accept a single command.",
    command: "ENROLL_TOKEN=… docker compose -f deploy/agent-only.yaml up -d",
  },
  {
    title: "Point it at your Git repo",
    body: "Compose files under stacks/<host>/<stack>/. Havenry reads them, compares, reports. It changes nothing until you say so.",
  },
];

const ROLES = [
  ["admin", "everything, including users"],
  ["operator", "start, stop, logs — on their hosts only"],
  ["viewer", "read, nothing else"],
];

export default function Setup() {
  return (
    <section id="how" className="wrap section setup">
      <h2 className="section__title">Up in three moves</h2>
      <p className="section__lede">
        Order matters here: no control plane, no token. No token, no host.
      </p>

      <div className="setup__grid">
        <StepRail steps={STEPS} />

        <div className="setup__aside">
          <Panel tone="var(--marigold)" tilt={0.7}>
            <h3 className="setup__asideTitle">Your files stay yours</h3>
            <p className="setup__asideText">
              No custom format, no database that owns your setup. Delete Havenry
              tomorrow and every stack keeps running. That is the whole deal.
            </p>
          </Panel>

          <Panel tilt={-0.5}>
            <h3 className="setup__asideTitle">Who gets to touch what</h3>
            <dl className="setup__roles">
              {ROLES.map(([role, what]) => (
                <div className="setup__role" key={role}>
                  <dt className="mono">{role}</dt>
                  <dd>{what}</dd>
                </div>
              ))}
            </dl>
            <p className="setup__note">
              Every action lands in the event log with the name of whoever did it.
            </p>
          </Panel>
        </div>
      </div>
    </section>
  );
}
