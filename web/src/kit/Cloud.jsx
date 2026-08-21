/**
 * Cloud — the base shape of the kit.
 *
 * Deliberately one SVG path rather than three stacked circles: with stacked
 * circles the outline runs straight through the middle of the shape. A single
 * path is the only way to get one continuous stroke around the silhouette.
 *
 * Children render on top of the shape, so anything can sit inside a cloud.
 */
export default function Cloud({
  width = 220,
  tone = "var(--cloud)",
  outline = "var(--ink)",
  strokeWidth = 5,
  children,
  className = "",
  style = {},
  ...rest
}) {
  const height = width * 0.62;

  return (
    <div
      className={className}
      style={{ position: "relative", width, height, ...style }}
      {...rest}
    >
      <svg
        viewBox="0 0 200 124"
        width={width}
        height={height}
        style={{ position: "absolute", inset: 0, overflow: "visible" }}
        aria-hidden="true"
        focusable="false"
      >
        <path
          d="M46 112
             C20 112 6 96 6 78
             C6 62 18 49 34 47
             C36 25 55 9 79 9
             C99 9 116 21 123 38
             C129 33 137 30 146 30
             C168 30 185 47 185 68
             C185 93 167 112 142 112
             Z"
          fill={tone}
          stroke={outline}
          strokeWidth={strokeWidth}
          strokeLinejoin="round"
        />
      </svg>
      {children}
    </div>
  );
}
