import { useState } from "react";
import "./PuffButton.css";

/**
 * PuffButton — the kit's only control.
 *
 * On press the shadow disappears and the surface moves by exactly the same
 * distance, so the button physically sinks instead of just changing colour.
 * That is the one place this kit spends motion on interaction.
 *
 * `as` lets the same visual be a <button> or an <a> without duplicating styles;
 * a link that navigates should stay a link.
 */
export default function PuffButton({
  children,
  variant = "primary",
  size = "md",
  as: Tag = "button",
  className = "",
  ...rest
}) {
  const [pressed, setPressed] = useState(false);

  return (
    <Tag
      className={`puff puff--${variant} puff--${size} ${pressed ? "is-pressed" : ""} ${className}`}
      onPointerDown={() => setPressed(true)}
      onPointerUp={() => setPressed(false)}
      onPointerLeave={() => setPressed(false)}
      onKeyDown={(e) => {
        if (e.key === " " || e.key === "Enter") setPressed(true);
      }}
      onKeyUp={() => setPressed(false)}
      onBlur={() => setPressed(false)}
      {...rest}
    >
      {children}
    </Tag>
  );
}
