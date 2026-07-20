import React, { Component, ErrorInfo, ReactNode, createRef } from 'react';
import { AlertTriangle, LayoutDashboard, RotateCcw } from 'lucide-react';
import Button from './ui/Button';

type Props = {
  children: ReactNode;
  pathname: string;
  onDashboard: () => void;
};

type State = {
  failed: boolean;
  attempt: number;
};

const SAFE_ROUTE_SEGMENTS = new Set([
  'activity', 'approvals', 'auth-providers', 'builder', 'credentials', 'execution-packs',
  'inventories', 'jobs', 'organizations', 'org', 'projects', 'runs', 'schedules',
  'service-principals', 'settings', 'teams', 'templates', 'tokens', 'users', 'workflows',
]);

export const safeRoutePattern = (pathname: string) => {
  const segments = pathname.split(/[?#]/, 1)[0].split('/').filter(Boolean);
  if (segments.length === 0) return '/';
  return `/${segments.slice(0, 6).map(segment => SAFE_ROUTE_SEGMENTS.has(segment) ? segment : ':id').join('/')}`;
};

/**
 * Contains render failures to the current route. The authenticated shell lives
 * outside this boundary, so navigation and sign-out remain available.
 */
export class RouteErrorBoundary extends Component<Props, State> {
  state: State = { failed: false, attempt: 0 };
  private headingRef = createRef<HTMLHeadingElement>();

  static getDerivedStateFromError(): Partial<State> {
    return { failed: true };
  }

  componentDidCatch(_error: Error, _info: ErrorInfo) {
    // Never log the caught error object: route exceptions can contain response
    // bodies, credentials, or host data. The pathname is sufficient correlation.
    console.error('Praetor route rendering failed', {
      route: safeRoutePattern(this.props.pathname),
    });
  }

  componentDidUpdate(previousProps: Props, previousState: State) {
    if (previousProps.pathname !== this.props.pathname && this.state.failed) {
      this.setState({ failed: false, attempt: 0 });
      return;
    }
    if (!previousState.failed && this.state.failed) {
      this.headingRef.current?.focus();
    }
  }

  private retry = () => {
    this.setState(({ attempt }) => ({ failed: false, attempt: attempt + 1 }));
  };

  render() {
    if (!this.state.failed) {
      return <React.Fragment key={this.state.attempt}>{this.props.children}</React.Fragment>;
    }

    const route = safeRoutePattern(this.props.pathname);
    return (
      <main className="flex min-h-full items-center justify-center px-6 py-12 max-[520px]:px-4" data-testid="route-recovery">
        <section className="w-full max-w-[600px]" aria-labelledby="route-error-title">
          <div className="mb-5 grid h-10 w-10 place-items-center rounded-xl bg-err/10 text-err" aria-hidden="true">
            <AlertTriangle size={20} />
          </div>
          <h1
            id="route-error-title"
            ref={this.headingRef}
            tabIndex={-1}
            className="text-xl font-semibold tracking-tight text-ink"
          >
            This page couldn’t be displayed
          </h1>
          <p className="mt-2 max-w-[62ch] text-sm leading-6 text-mut">
            Praetor kept the application shell available. Retry this page or return to Dashboard.
          </p>

          <div className="mt-6 border-y border-line py-4">
            <p className="text-xs font-medium text-dim">Affected page</p>
            <code className="mt-1.5 block break-all font-mono text-xs text-ink2" data-testid="failed-route">
              {route}
            </code>
          </div>

          <div className="mt-6 flex flex-wrap gap-3">
            <Button onClick={this.retry} icon={<RotateCcw size={15} />}>
              Retry page
            </Button>
            <Button variant="secondary" onClick={this.props.onDashboard} icon={<LayoutDashboard size={15} />}>
              Go to Dashboard
            </Button>
          </div>
          <p className="mt-5 text-xs leading-5 text-dim">
            If this keeps happening, report the page path and time. Sensitive error details are intentionally hidden.
          </p>
        </section>
      </main>
    );
  }
}

export default RouteErrorBoundary;
