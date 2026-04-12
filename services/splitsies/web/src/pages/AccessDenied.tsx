export default function AccessDenied() {
  return (
    <div className="flex items-center justify-center min-h-screen bg-red-600">
      <div className="text-center text-white">
        <h1 className="text-5xl font-bold mb-4">Access Denied</h1>
        <p className="text-xl text-red-100 mb-8">
          Your email has not been approved for Splitsies.
        </p>
        <p className="text-red-200">
          Ask an admin to add your Google email to the whitelist.
        </p>
      </div>
    </div>
  );
}
