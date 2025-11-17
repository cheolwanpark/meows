/**
 * Toast notification wrapper for Toastify-js
 * Provides themed toast notifications integrated with the application's design system
 */
(function() {
  'use strict';

  // Check if Toastify is loaded
  if (typeof Toastify === 'undefined') {
    console.error('Toastify is not loaded. Please include toastify-js before this script.');
    return;
  }

  /**
   * Default configuration for all toasts
   */
  const TOAST_DURATION_MS = 3000;
  const defaultConfig = {
    duration: TOAST_DURATION_MS,
    gravity: "top",
    position: "right",
    close: true,
    stopOnFocus: true,
    ariaLive: "polite",
    escapeMarkup: true, // Prevent XSS by escaping HTML
    offset: {
      x: 16,
      y: 16
    }
  };

  /**
   * Show a success toast notification
   * @param {string} message - The message to display
   * @param {object} options - Optional configuration overrides
   * @returns {object} Toastify instance
   */
  function success(message, options = {}) {
    return Toastify({
      text: message,
      className: "toast-success",
      ...defaultConfig,
      ...options
    }).showToast();
  }

  /**
   * Show an error toast notification
   * @param {string} message - The error message to display
   * @param {object} options - Optional configuration overrides
   * @returns {object} Toastify instance
   */
  function error(message, options = {}) {
    return Toastify({
      text: message,
      className: "toast-error",
      ...defaultConfig,
      ariaLive: "assertive", // More urgent for errors (override default)
      ...options
    }).showToast();
  }

  /**
   * Show a warning toast notification
   * @param {string} message - The warning message to display
   * @param {object} options - Optional configuration overrides
   * @returns {object} Toastify instance
   */
  function warning(message, options = {}) {
    return Toastify({
      text: message,
      className: "toast-warning",
      ...defaultConfig,
      ...options
    }).showToast();
  }

  /**
   * Show an info toast notification
   * @param {string} message - The info message to display
   * @param {object} options - Optional configuration overrides
   * @returns {object} Toastify instance
   */
  function info(message, options = {}) {
    return Toastify({
      text: message,
      className: "toast-info",
      ...defaultConfig,
      ...options
    }).showToast();
  }

  /**
   * Show a loading toast notification
   * Returns an object with a hide() method for programmatic dismissal
   * @param {string} message - The loading message to display
   * @param {object} options - Optional configuration overrides
   * @returns {object} Object with hide() method
   */
  function loading(message, options = {}) {
    const toastInstance = Toastify({
      text: message || "Loading...",
      className: "toast-loading",
      ...defaultConfig,
      duration: 600000, // 10 minutes (very long, meant to be dismissed manually)
      ...options
    });

    toastInstance.showToast();

    return {
      hide: function() {
        if (toastInstance && typeof toastInstance.hideToast === 'function') {
          toastInstance.hideToast();
        }
      }
    };
  }

  /**
   * Generic toast function that accepts a type parameter
   * Used by HTMX event handlers
   * @param {string} message - The message to display
   * @param {string} type - Toast type: 'success', 'error', 'warning', 'info'
   * @param {object} options - Optional configuration overrides
   * @returns {object} Toastify instance
   * @note Does not support 'loading' type - use loading() directly for dismissible toasts
   */
  function show(message, type = 'info', options = {}) {
    const toastFunctions = {
      success: success,
      error: error,
      warning: warning,
      info: info
    };

    const toastFn = toastFunctions[type] || info;
    return toastFn(message, options);
  }

  // Expose the toast API globally
  window.toast = {
    success: success,
    error: error,
    warning: warning,
    info: info,
    loading: loading,
    show: show
  };
})();
