/**
 * Global CSRF Token Manager
 *
 * Provides centralized CSRF token management for the application.
 * Single source of truth: meta tag in <head>
 *
 * Usage:
 * - HTMX requests: Automatic via global event listener in base.templ
 * - Manual fetch/Alpine.js: window.csrf.getToken() or window.csrf.getHeaders()
 *
 * Example:
 *   fetch('/api/endpoint', {
 *     method: 'POST',
 *     headers: {
 *       'Content-Type': 'application/json',
 *       ...window.csrf.getHeaders()
 *     },
 *     body: JSON.stringify(data)
 *   })
 */
(function() {
    'use strict';

    /**
     * CSRF Token Manager Singleton
     */
    window.csrf = {
        /**
         * Get the current CSRF token from meta tag
         * @returns {string|null} The CSRF token or null if not found
         */
        getToken: function() {
            const metaTag = document.querySelector('meta[name="csrf-token"]');
            if (!metaTag) {
                console.error('[CSRF] Meta tag <meta name="csrf-token"> not found in document head!');
                console.error('[CSRF] Ensure base template includes CSRF token meta tag');
                return null;
            }

            const token = metaTag.getAttribute('content');
            if (!token || token.trim() === '') {
                console.error('[CSRF] Meta tag found but token value is empty');
                return null;
            }

            return token;
        },

        /**
         * Get headers object with CSRF token for fetch requests
         * @returns {Object} Headers object with X-CSRF-Token
         */
        getHeaders: function() {
            const token = this.getToken();
            if (!token) {
                console.warn('[CSRF] Cannot create headers - token not available');
                return {};
            }

            return {
                'X-CSRF-Token': token
            };
        },

        /**
         * Refresh the CSRF token from the server
         * Fetches a new token and updates the meta tag
         * @returns {Promise<string|null>} The new token or null on failure
         */
        refreshToken: async function() {
            try {
                console.log('[CSRF] Refreshing token from server...');

                const response = await fetch('/api/csrf-token', {
                    method: 'GET',
                    credentials: 'same-origin', // Important: send cookies
                    headers: {
                        'Accept': 'application/json'
                    }
                });

                if (!response.ok) {
                    throw new Error(`HTTP ${response.status}: ${response.statusText}`);
                }

                const data = await response.json();

                if (!data.token) {
                    throw new Error('Server response missing token field');
                }

                // Update meta tag with new token
                let metaTag = document.querySelector('meta[name="csrf-token"]');
                if (!metaTag) {
                    // Create meta tag if it doesn't exist
                    metaTag = document.createElement('meta');
                    metaTag.name = 'csrf-token';
                    document.head.appendChild(metaTag);
                    console.log('[CSRF] Created missing meta tag');
                }

                metaTag.setAttribute('content', data.token);
                console.log('[CSRF] Token refreshed successfully');

                return data.token;

            } catch (error) {
                console.error('[CSRF] Token refresh failed:', error);
                console.error('[CSRF] This may cause subsequent requests to fail with 403 Forbidden');
                return null;
            }
        },

        /**
         * Initialize the CSRF manager
         * Sets up event listeners and validates token availability
         */
        init: function() {
            // Validate token exists on page load
            const token = this.getToken();
            if (token) {
                console.log('[CSRF] Manager initialized - token available');
            } else {
                console.warn('[CSRF] Manager initialized but token NOT available!');
            }
        }
    };

    // Auto-initialize when DOM is ready
    if (document.readyState === 'loading') {
        document.addEventListener('DOMContentLoaded', function() {
            window.csrf.init();
        });
    } else {
        // DOM already loaded
        window.csrf.init();
    }

})();
