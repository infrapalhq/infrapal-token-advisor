import { Component, type ErrorInfo, type ReactNode } from "react";

// ErrorBoundary keeps one page's crash from blanking the whole dashboard. It
// shows what went wrong and a way back instead.
export class ErrorBoundary extends Component<{ children: ReactNode }, { error: Error | null }> {
  state = { error: null as Error | null };

  static getDerivedStateFromError(error: Error) {
    return { error };
  }

  componentDidCatch(error: Error, info: ErrorInfo) {
    console.error("page crashed", error, info.componentStack);
  }

  render() {
    if (!this.state.error) return this.props.children;
    return (
      <div className="shell">
        <main className="main">
          <div className="empty" role="alert">
            <strong>This page hit a problem</strong>
            <span>{this.state.error.message}</span>
            <a className="btn" href="/">← Back to Overview</a>
          </div>
        </main>
      </div>
    );
  }
}
