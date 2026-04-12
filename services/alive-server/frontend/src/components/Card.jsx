export default function Card({ label, children, className = '' }) {
  return (
    <section className={`card ${className}`}>
      {label && <div className="card-label">{label}</div>}
      {children}
    </section>
  )
}
