import "./Panel.css";

/**
 * Panel — a container with corners.
 *
 * Text with edges needs edges: prose inside a cloud silhouette sits badly and
 * fights its own line lengths. Panel speaks the same visual language as Cloud
 * without pretending to be one.
 *
 * `tilt` takes degrees. Keep it under about 1.5° — beyond that it stops
 * reading as hand-placed and starts reading as broken.
 */
export default function Panel({
  children,
  tone = "var(--cloud)",
  tilt = 0,
  className = "",
  style = {},
  ...rest
}) {
  return (
    <div
      className={`panel ${className}`}
      style={{
        background: tone,
        transform: tilt ? `rotate(${tilt}deg)` : undefined,
        ...style,
      }}
      {...rest}
    >
      {children}
    </div>
  );
}
