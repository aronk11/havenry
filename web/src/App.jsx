import Hero from "./sections/Hero.jsx";
import Features from "./sections/Features.jsx";
import Boundaries from "./sections/Boundaries.jsx";
import Setup from "./sections/Setup.jsx";
import KitShowcase from "./sections/KitShowcase.jsx";
import Footer from "./sections/Footer.jsx";
import "./styles/sections.css";

export default function App() {
  return (
    <>
      <a className="skip" href="#how">
        Skip to setup
      </a>
      <main id="top">
        <Hero />
        <Features />
        <Boundaries />
        <Setup />
        <KitShowcase />
      </main>
      <Footer />
    </>
  );
}
