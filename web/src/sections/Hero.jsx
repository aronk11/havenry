import { Cloud, PuffButton, Sky, Tag } from "../kit";
import DriftSky from "./DriftSky.jsx";
import { REPO_URL } from "../config.js";
import "./Hero.css";

export default function Hero() {
  return (
    <Sky>
      <div className="wrap hero">
        <header className="hero__bar">
          <a className="hero__brand" href="#top">
            <Cloud width={44} />
            <span className="display hero__wordmark">Havenry</span>
          </a>
          <nav className="hero__nav" aria-label="Primary">
            <a href="#how">How it works</a>
            <a href="#kit">Design kit</a>
            <PuffButton as="a" href={REPO_URL} variant="cloud">
              GitHub
            </PuffButton>
          </nav>
        </header>

        <div className="hero__pitch">
          <Tag tone="sun">Docker · self-hosted · AGPL-3.0</Tag>
          <h1 className="hero__headline">
            {/* Das ausdrückliche {" "} ist nötig: JSX entfernt Leerraum rund um
                Elemente. Wird der Umbruch auf schmalen Schirmen ausgeblendet,
                stünde sonst "keepin" da. */}
            The cloud you keep{" "}
            <br className="hero__break" />
            in your basement
          </h1>
          <p className="hero__sub">
            Havenry watches every Docker host you own, keeps Git as the record of
            what should be running, and tells you the moment the two stop
            agreeing. It never quietly undoes your tinkering.
          </p>
          <div className="hero__cta">
            <PuffButton size="lg" as="a" href="#how">
              Set it up
            </PuffButton>
            <PuffButton size="lg" variant="ghost" as="a" href={REPO_URL}>
              Read the code
            </PuffButton>
          </div>
        </div>

        <DriftSky />
      </div>
    </Sky>
  );
}
