import "./Tag.css";

/**
 * Tag — status pill.
 *
 * Colour never carries the meaning on its own; the label always repeats it.
 * "in sync" reads the same in greyscale, to a colour-blind visitor, and in a
 * screenshot someone pastes into a forum.
 */
export default function Tag({ tone = "neutral", children, ...rest }) {
  return (
    <span className={`tag tag--${tone}`} {...rest}>
      {children}
    </span>
  );
}
