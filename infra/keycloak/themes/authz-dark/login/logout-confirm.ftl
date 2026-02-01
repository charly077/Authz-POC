<#import "template.ftl" as layout>
<@layout.registrationLayout; section>
    <#if section = "header">
    <#elseif section = "form">
    <div class="split-login-wrapper">
        <div class="split-login-card">
            <!-- Left Panel: Form -->
            <div class="login-form-panel">
                <div class="brand-mark">
                    <svg width="32" height="32" viewBox="0 0 32 32" fill="none">
                        <rect width="32" height="32" rx="8" fill="#5b4cd4"/>
                        <path d="M10 16.5L14 20.5L22 12.5" stroke="white" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"/>
                    </svg>
                    <span class="brand-name">AuthZ</span>
                </div>

                <div class="welcome-text">
                    <h1>Sign<br/>Out</h1>
                    <p>${msg("logoutConfirmTitle")}</p>
                </div>

                <#if message?has_content && (message.type != 'warning' || !isAppInitiatedAction??)>
                    <div class="login-alert login-alert-${message.type}">
                        <span>${kcSanitize(message.summary)?no_esc}</span>
                    </div>
                </#if>

                <div class="logout-description">
                    <p>You will be signed out of your authorization workspace. Any active sessions will be terminated.</p>
                </div>

                <div class="form-actions">
                    <form class="form-inline" method="post" action="${url.logoutConfirmAction}">
                        <input type="hidden" name="session_code" value="${logoutConfirm.code}">
                        <button name="confirmLogout" id="kc-logout" type="submit" class="btn-signin btn-logout-confirm">${msg("doLogout")}</button>
                    </form>
                </div>

                <#if logoutConfirm.skipLink>
                <#else>
                    <#if (client.baseUrl)?has_content>
                        <div class="back-link">
                            <a href="${client.baseUrl}">Back to application</a>
                        </div>
                    </#if>
                </#if>
            </div>

            <!-- Right Panel: Decorative -->
            <div class="login-illustration-panel">
                <div class="illustration-bg">
                    <!-- Cloud shapes -->
                    <svg class="cloud cloud-1" viewBox="0 0 200 80" fill="none">
                        <ellipse cx="60" cy="50" rx="60" ry="30" fill="rgba(255,255,255,0.12)"/>
                        <ellipse cx="100" cy="40" rx="50" ry="28" fill="rgba(255,255,255,0.12)"/>
                        <ellipse cx="140" cy="50" rx="55" ry="30" fill="rgba(255,255,255,0.12)"/>
                    </svg>
                    <svg class="cloud cloud-2" viewBox="0 0 180 70" fill="none">
                        <ellipse cx="50" cy="45" rx="50" ry="25" fill="rgba(255,255,255,0.08)"/>
                        <ellipse cx="90" cy="35" rx="45" ry="24" fill="rgba(255,255,255,0.08)"/>
                        <ellipse cx="130" cy="45" rx="48" ry="25" fill="rgba(255,255,255,0.08)"/>
                    </svg>
                    <svg class="cloud cloud-3" viewBox="0 0 160 60" fill="none">
                        <ellipse cx="45" cy="38" rx="45" ry="22" fill="rgba(255,255,255,0.06)"/>
                        <ellipse cx="80" cy="30" rx="40" ry="20" fill="rgba(255,255,255,0.06)"/>
                        <ellipse cx="115" cy="38" rx="43" ry="22" fill="rgba(255,255,255,0.06)"/>
                    </svg>

                    <!-- Door/exit icon -->
                    <svg class="deco-lock" viewBox="0 0 80 100" fill="none">
                        <rect x="10" y="15" width="44" height="70" rx="4" fill="rgba(255,255,255,0.12)" stroke="rgba(255,255,255,0.25)" stroke-width="2"/>
                        <rect x="18" y="20" width="28" height="6" rx="3" fill="rgba(255,255,255,0.2)"/>
                        <circle cx="44" cy="50" r="4" fill="rgba(255,255,255,0.3)"/>
                        <path d="M52 40L68 50L52 60" stroke="rgba(255,255,255,0.35)" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round" fill="none"/>
                        <line x1="44" y1="50" x2="66" y2="50" stroke="rgba(255,255,255,0.3)" stroke-width="2.5" stroke-linecap="round"/>
                    </svg>

                    <!-- Shield -->
                    <svg class="deco-shield" viewBox="0 0 80 96" fill="none">
                        <path d="M40 8L12 24V48C12 68 24 84 40 90C56 84 68 68 68 48V24L40 8Z" fill="rgba(255,255,255,0.1)" stroke="rgba(255,255,255,0.25)" stroke-width="2"/>
                        <path d="M32 48L38 54L50 42" stroke="rgba(255,255,255,0.4)" stroke-width="3" stroke-linecap="round" stroke-linejoin="round"/>
                    </svg>

                    <!-- Wave goodbye icon -->
                    <svg class="deco-check" viewBox="0 0 56 56" fill="none">
                        <circle cx="28" cy="28" r="24" fill="rgba(255,255,255,0.18)"/>
                        <path d="M20 32C22 28 26 26 28 26C30 26 30 30 30 30C30 30 30 26 32 26C34 26 36 28 36 32" stroke="white" stroke-width="2" stroke-linecap="round" opacity="0.5" fill="none"/>
                        <line x1="28" y1="22" x2="28" y2="18" stroke="white" stroke-width="2" stroke-linecap="round" opacity="0.4"/>
                        <line x1="22" y1="24" x2="20" y2="20" stroke="white" stroke-width="2" stroke-linecap="round" opacity="0.3"/>
                        <line x1="34" y1="24" x2="36" y2="20" stroke="white" stroke-width="2" stroke-linecap="round" opacity="0.3"/>
                    </svg>

                    <!-- Person silhouette -->
                    <svg class="deco-person" viewBox="0 0 100 160" fill="none">
                        <circle cx="50" cy="30" r="18" fill="rgba(255,255,255,0.2)"/>
                        <path d="M20 140C20 108 32 82 50 82C68 82 80 108 80 140" fill="rgba(255,255,255,0.15)"/>
                    </svg>

                    <!-- Decorative circles -->
                    <div class="deco-circle deco-circle-1"></div>
                    <div class="deco-circle deco-circle-2"></div>
                    <div class="deco-circle deco-circle-3"></div>

                    <!-- Floating particles -->
                    <div class="particle particle-1"></div>
                    <div class="particle particle-2"></div>
                    <div class="particle particle-3"></div>
                    <div class="particle particle-4"></div>
                    <div class="particle particle-5"></div>
                </div>
            </div>
        </div>
    </div>
    </#if>
</@layout.registrationLayout>
