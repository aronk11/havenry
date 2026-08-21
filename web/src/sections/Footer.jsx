import { Cloud, PuffButton } from "../kit";
import { ADR_URL, LICENSE, REPO_URL } from "../config.js";
import "./Footer.css";

export default function Footer() {
  return (
    <footer className="footer">
      <div className="wrap">
        <div className="footer__close">
          <h2 className="footer__title">Go and know what's running</h2>
          <PuffButton size="lg" as="a" href={REPO_URL}>
            Get Havenry
          </PuffButton>
        </div>

        <div className="footer__base">
          <div className="footer__brand">
            <Cloud width={30} />
            <span className="display footer__wordmark">Havenry</span>
          </div>
          <nav className="footer__links" aria-label="Footer">
            {/* AGPL §13: a network user must be able to reach the source of the
                running version. On the site itself this is simply the polite
                version of the same obligation. */}
            <a href={REPO_URL}>Source</a>
            <a href={ADR_URL}>Decision records</a>
            <span>{LICENSE}</span>
          </nav>
        </div>
      </div>
    </footer>
  );
}
