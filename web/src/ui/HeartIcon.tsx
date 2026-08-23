export function HeartIcon(props: { filled: boolean }) {
  return (
    <svg
      viewBox="0 0 24 24"
      fill={props.filled ? "currentColor" : "none"}
      stroke={props.filled ? "none" : "currentColor"}
      stroke-width="1.7"
      stroke-linejoin="round"
      aria-hidden="true"
    >
      <path d="M12 20.3C10.4 19 3.6 14.6 3.6 9.8A4.4 4.4 0 0112 7.6a4.4 4.4 0 018.4 2.2c0 4.8-6.8 9.2-8.4 10.5z" />
    </svg>
  );
}
