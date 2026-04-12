// variant: 'primary' | 'ghost' | 'danger'
export default function Button({
  variant = 'primary',
  type = 'button',
  children,
  ...rest
}) {
  return (
    <button type={type} className={`btn btn-${variant}`} {...rest}>
      {children}
    </button>
  )
}
