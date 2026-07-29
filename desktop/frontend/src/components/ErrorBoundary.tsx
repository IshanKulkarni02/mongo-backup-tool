import { Component, type ErrorInfo, type PropsWithChildren } from "react";
import { AlertTriangle } from "lucide-react";
import "./ErrorBoundary.css";

interface State {
  error: Error | null;
}

// Wraps a subtree so a render error in one view (a bad chart, an
// unexpected data shape from a driver, etc.) shows a recoverable message
// in that view's place instead of unmounting the whole app to a blank
// screen — React discards everything above the nearest error boundary by
// default, and this codebase had none anywhere before this.
export class ErrorBoundary extends Component<PropsWithChildren<{ label?: string }>, State> {
  state: State = { error: null };

  static getDerivedStateFromError(error: Error): State {
    return { error };
  }

  componentDidCatch(error: Error, info: ErrorInfo) {
    console.error(`[ErrorBoundary${this.props.label ? `: ${this.props.label}` : ""}]`, error, info.componentStack);
  }

  reset = () => this.setState({ error: null });

  render() {
    if (this.state.error) {
      return (
        <div className="error-boundary">
          <AlertTriangle size={28} />
          <div className="error-boundary-title">Something went wrong{this.props.label ? ` in ${this.props.label}` : ""}</div>
          <div className="error-boundary-message">{this.state.error.message || String(this.state.error)}</div>
          <button className="error-boundary-retry" onClick={this.reset}>
            Try again
          </button>
        </div>
      );
    }
    return this.props.children;
  }
}
