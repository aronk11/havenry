import Cloud from "./Cloud.jsx";
import "./HostCloud.css";

/**
 * HostCloud — a machine, drawn as a cloud.
 *
 * This is the idea the whole page rests on: the cloud is not decoration, it is
 * the host. The squares inside are its containers. Once a visitor reads that
 * once, the rest of the illustration explains itself.
 *
 * `drift` is 0 or 1 rather than a pixel offset, so callers describe state and
 * the component owns the movement.
 */
export default function HostCloud({
  name,
  services = [],
  drifted = false,
  drift = 0,
  width = 200,
}) {
  return (
    <div
      className="hostcloud"
      style={{ "--drift": drift }}
      data-drifted={drifted || undefined}
    >
      <Cloud width={width} outline={drifted ? "var(--coral)" : "var(--ink)"}>
        <div className="hostcloud__body" style={{ paddingTop: width * 0.07 }}>
          <span className="mono hostcloud__name" style={{ fontSize: width * 0.062 }}>
            {name}
          </span>
          <div className="hostcloud__services">
            {services.map((s, i) => (
              <span
                key={s.label ?? i}
                className="hostcloud__dot"
                data-ok={s.ok || undefined}
                style={{ width: width * 0.055, height: width * 0.055 }}
                title={s.label}
              />
            ))}
          </div>
        </div>
      </Cloud>
    </div>
  );
}
