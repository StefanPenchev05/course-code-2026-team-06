import { useState } from 'react';
import { Link, NavLink, Outlet, useNavigate } from 'react-router';
import { useAuth } from '../features/auth/AuthContext';
import { hasRole } from '../features/auth/types';
import { Button } from '../shared/ui';

export function AppLayout() {
  const { user, logout } = useAuth();
  const navigate = useNavigate();
  const [loggingOut, setLoggingOut] = useState(false);

  async function handleLogout() {
    setLoggingOut(true);
    try {
      await logout();
      navigate('/login', { replace: true });
    } finally {
      setLoggingOut(false);
    }
  }

  return (
    <div className="app">
      <a className="skip-link" href="#main">
        Skip to content
      </a>
      <header className="app-header">
        <Link to="/" className="app-header__brand">
          Tutti
        </Link>
        <nav aria-label="Main" className="app-header__nav">
          {user ? (
            <>
              <NavLink to="/" end>
                Home
              </NavLink>
              {user.verified_student && (
                <>
                  <NavLink to="/tutors">Find a tutor</NavLink>
                  <NavLink to="/sessions">My sessions</NavLink>
                </>
              )}
              {hasRole(user, 'tutor') && <NavLink to="/tutor">Teaching</NavLink>}
              {hasRole(user, 'coordinator') && <NavLink to="/coordinator">Coordinator</NavLink>}
              <Button variant="link" onClick={() => void handleLogout()} loading={loggingOut}>
                Log out
              </Button>
            </>
          ) : (
            <>
              <NavLink to="/login">Log in</NavLink>
              <NavLink to="/register">Register</NavLink>
            </>
          )}
        </nav>
      </header>
      <main id="main" className="app-main" tabIndex={-1}>
        <Outlet />
      </main>
    </div>
  );
}
