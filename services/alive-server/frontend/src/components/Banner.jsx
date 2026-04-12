// Domain-expiry banner. Hidden when severity === 'ok' or no expiry data.
export default function Banner({ expiry }) {
  if (!expiry || expiry.severity === 'ok' || expiry.severity === 'unknown') {
    return null
  }
  return (
    <div className={`banner banner-${expiry.severity}`}>
      ⚠ domain <strong>attlas.uk</strong> expires in {expiry.days_remaining}{' '}
      {expiry.days_remaining === 1 ? 'day' : 'days'} ({expiry.expires_at})
      <a
        href="https://dash.cloudflare.com"
        target="_blank"
        rel="noopener noreferrer"
      >
        renew
      </a>
    </div>
  )
}
