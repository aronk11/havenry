import { useEffect, useState } from "react";
import { HostCloud, PuffButton, Tag } from "../kit";
import "./DriftSky.css";

/**
 * DriftSky — the signature element.
 *
 * One cloud leaves formation; you either bring it back or keep the change.
 * Those are the only two things Havenry offers when it finds drift, so this is
 * the product argument as a toy rather than a screenshot of a dashboard.
 *
 * It drifts on its own after a moment because a visitor who has to discover an
 * interaction usually never does. Once they have answered once, it stops
 * nagging.
 */
const HOSTS = [
  { name: "nas-01", width: 172, services: [{ ok: true, label: "jellyfin" }, { ok: true, label: "sonarr" }] },
  { name: "pi-03", width: 156, services: [{ ok: true, label: "pihole" }] },
];

export default function DriftSky() {
  const [drifted, setDrifted] = useState(false);
  const [resolved, setResolved] = useState(null);

  useEffect(() => {
    if (resolved) return undefined;
    const t = setTimeout(() => setDrifted(true), 2600);
    return () => clearTimeout(t);
  }, [resolved]);

  const resolve = (how) => {
    setDrifted(false);
    setResolved(how);
  };

  const message = resolved
    ? resolved === "revert"
      ? "Reverted. nas-02 is back on the version in Git."
      : "Adopted. Git now records what nas-02 was already running."
    : drifted
      ? "nas-02 drifted — caddy runs 2.7 here, Git says 2.8."
      : "All three hosts match Git.";

  return (
    <section className="driftsky" aria-label="Drift demonstration">
      <div className="driftsky__formation">
        <HostCloud {...HOSTS[0]} />
        <HostCloud
          name="nas-02"
          width={200}
          drifted={drifted}
          drift={drifted ? 1 : 0}
          services={[
            { ok: !drifted, label: "caddy" },
            { ok: true, label: "vaultwarden" },
          ]}
        />
        <HostCloud {...HOSTS[1]} />
      </div>

      <div className="driftsky__readout">
        <p className="driftsky__status" aria-live="polite">
          <Tag tone={drifted ? "warn" : "good"}>{drifted ? "1 drift" : "in sync"}</Tag>
          <span className="driftsky__message">{message}</span>
        </p>

        <div className="driftsky__actions">
          {drifted && (
            <>
              <PuffButton variant="cloud" onClick={() => resolve("revert")}>
                Bring it back
              </PuffButton>
              <PuffButton variant="primary" onClick={() => resolve("adopt")}>
                Keep the change
              </PuffButton>
            </>
          )}

          {resolved && (
            <PuffButton variant="ghost" onClick={() => setResolved(null)}>
              Run it again
            </PuffButton>
          )}

          {!drifted && !resolved && (
            <span className="driftsky__hint">Give it a moment…</span>
          )}
        </div>
      </div>
    </section>
  );
}
