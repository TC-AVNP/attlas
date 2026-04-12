import { Link, useLocation } from "react-router-dom";
import { api } from "../api/client";
import type { User } from "../api/types";

export default function Layout({
  user,
  children,
}: {
  user: User;
  children: React.ReactNode;
}) {
  const location = useLocation();

  const navItems = [
    { to: "/", label: "Home" },
    { to: "/groups", label: "Groups" },
    { to: "/overview", label: "Overview" },
  ];
  if (user.is_admin) {
    navItems.push({ to: "/admin", label: "Admin" });
  }

  const handleLogout = async () => {
    await api.logout();
    window.location.href = "/";
  };

  return (
    <div className="min-h-screen bg-gray-50">
      <nav className="bg-white border-b border-gray-200 px-4 py-3">
        <div className="max-w-4xl mx-auto flex items-center justify-between">
          <div className="flex items-center gap-6">
            <Link to="/" className="text-xl font-bold text-brand">
              Splitsies
            </Link>
            <div className="flex gap-4">
              {navItems.map((item) => (
                <Link
                  key={item.to}
                  to={item.to}
                  className={`text-sm font-medium ${
                    location.pathname === item.to
                      ? "text-brand"
                      : "text-gray-500 hover:text-gray-700"
                  }`}
                >
                  {item.label}
                </Link>
              ))}
            </div>
          </div>
          <div className="flex items-center gap-3">
            <span className="text-sm text-gray-600">
              {user.name || user.email}
            </span>
            {user.picture && (
              <img
                src={user.picture}
                alt=""
                className="w-8 h-8 rounded-full"
              />
            )}
            <button
              onClick={handleLogout}
              className="text-sm text-gray-400 hover:text-gray-600"
            >
              Logout
            </button>
          </div>
        </div>
      </nav>
      <main className="max-w-4xl mx-auto px-4 py-6">{children}</main>
    </div>
  );
}
