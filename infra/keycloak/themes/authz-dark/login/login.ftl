<#import "template.ftl" as layout>
<@layout.registrationLayout displayMessage=!messagesPerField.existsError('username','password') displayInfo=realm.password && realm.registrationAllowed && !registrationDisabled??; section>
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
                    <h1>Welcome<br/>Back</h1>
                    <p>Sign in to your authorization workspace</p>
                </div>

                <#if message?has_content && (message.type != 'warning' || !isAppInitiatedAction??)>
                    <div class="login-alert login-alert-${message.type}">
                        <span>${kcSanitize(message.summary)?no_esc}</span>
                    </div>
                </#if>

                <#if realm.password>
                    <form id="kc-form-login" onsubmit="login.disabled = true; return true;" action="${url.loginAction}" method="post">
                        <#if !usernameHidden??>
                            <div class="form-field">
                                <label for="username"><#if !realm.loginWithEmailAllowed>${msg("username")}<#elseif !realm.registrationEmailAsUsername>${msg("usernameOrEmail")}<#else>${msg("email")}</#if></label>
                                <input tabindex="1" id="username" name="username" value="${(login.username!'')}" type="text" autofocus autocomplete="off"
                                       aria-invalid="<#if messagesPerField.existsError('username','password')>true</#if>"
                                       placeholder="Enter your username"
                                />
                                <#if messagesPerField.existsError('username','password')>
                                    <span class="field-error" aria-live="polite">
                                        ${kcSanitize(messagesPerField.getFirstError('username','password'))?no_esc}
                                    </span>
                                </#if>
                            </div>
                        </#if>

                        <div class="form-field">
                            <label for="password">${msg("password")}</label>
                            <input tabindex="2" id="password" name="password" type="password" autocomplete="off"
                                   aria-invalid="<#if messagesPerField.existsError('username','password')>true</#if>"
                                   placeholder="Enter your password"
                            />
                            <#if usernameHidden?? && messagesPerField.existsError('username','password')>
                                <span class="field-error" aria-live="polite">
                                    ${kcSanitize(messagesPerField.getFirstError('username','password'))?no_esc}
                                </span>
                            </#if>
                        </div>

                        <div class="form-options">
                            <#if realm.rememberMe && !usernameHidden??>
                                <label class="remember-me">
                                    <#if login.rememberMe??>
                                        <input tabindex="3" id="rememberMe" name="rememberMe" type="checkbox" checked>
                                    <#else>
                                        <input tabindex="3" id="rememberMe" name="rememberMe" type="checkbox">
                                    </#if>
                                    <span class="checkmark"></span>
                                    ${msg("rememberMe")}
                                </label>
                            </#if>
                            <#if realm.resetPasswordAllowed>
                                <a tabindex="5" class="forgot-link" href="${url.loginResetCredentialsUrl}">${msg("doForgotPassword")}</a>
                            </#if>
                        </div>

                        <div class="form-actions">
                            <input type="hidden" id="id-hidden-input" name="credentialId" <#if auth.selectedCredential?has_content>value="${auth.selectedCredential}"</#if>/>
                            <button tabindex="4" name="login" id="kc-login" type="submit" class="btn-signin">${msg("doLogIn")}</button>
                        </div>
                    </form>
                </#if>

                <#if realm.password && realm.registrationAllowed && !registrationDisabled??>
                    <div class="registration-link">
                        <span>${msg("noAccount")} <a tabindex="6" href="${url.registrationUrl}">${msg("doRegister")}</a></span>
                    </div>
                </#if>

                <#if realm.password && social.providers??>
                    <div class="social-login">
                        <div class="social-divider"><span>or continue with</span></div>
                        <div class="social-buttons">
                            <#list social.providers as p>
                                <a id="social-${p.alias}" class="social-btn" href="${p.loginUrl}">
                                    <#if p.iconClasses?has_content>
                                        <i class="${p.iconClasses!}" aria-hidden="true"></i>
                                    </#if>
                                    <span>${p.displayName!}</span>
                                </a>
                            </#list>
                        </div>
                    </div>
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

                    <!-- Lock icon -->
                    <svg class="deco-lock" viewBox="0 0 80 100" fill="none">
                        <rect x="8" y="40" width="64" height="52" rx="12" fill="rgba(255,255,255,0.15)" stroke="rgba(255,255,255,0.3)" stroke-width="2"/>
                        <path d="M24 40V28C24 17.5 30 10 40 10C50 10 56 17.5 56 28V40" stroke="rgba(255,255,255,0.3)" stroke-width="3" stroke-linecap="round" fill="none"/>
                        <circle cx="40" cy="62" r="6" fill="rgba(255,255,255,0.35)"/>
                        <rect x="38" y="66" width="4" height="10" rx="2" fill="rgba(255,255,255,0.35)"/>
                    </svg>

                    <!-- Fingerprint -->
                    <svg class="deco-fingerprint" viewBox="0 0 120 120" fill="none">
                        <path d="M60 20C38 20 24 38 24 60" stroke="rgba(255,255,255,0.12)" stroke-width="2" stroke-linecap="round" fill="none"/>
                        <path d="M60 28C42 28 32 42 32 60C32 72 36 82 44 88" stroke="rgba(255,255,255,0.15)" stroke-width="2" stroke-linecap="round" fill="none"/>
                        <path d="M60 36C46 36 40 48 40 60C40 76 48 86 56 92" stroke="rgba(255,255,255,0.18)" stroke-width="2" stroke-linecap="round" fill="none"/>
                        <path d="M60 44C52 44 48 52 48 60C48 72 52 80 60 88" stroke="rgba(255,255,255,0.22)" stroke-width="2" stroke-linecap="round" fill="none"/>
                        <path d="M60 52C56 52 54 56 54 60C54 68 56 74 60 80" stroke="rgba(255,255,255,0.28)" stroke-width="2" stroke-linecap="round" fill="none"/>
                        <path d="M96 60C96 38 82 20 60 20" stroke="rgba(255,255,255,0.12)" stroke-width="2" stroke-linecap="round" fill="none"/>
                        <path d="M88 60C88 42 78 28 60 28" stroke="rgba(255,255,255,0.15)" stroke-width="2" stroke-linecap="round" fill="none"/>
                        <path d="M80 60C80 46 74 36 60 36" stroke="rgba(255,255,255,0.18)" stroke-width="2" stroke-linecap="round" fill="none"/>
                        <path d="M72 60C72 50 68 44 60 44" stroke="rgba(255,255,255,0.22)" stroke-width="2" stroke-linecap="round" fill="none"/>
                        <path d="M66 60C66 56 64 52 60 52" stroke="rgba(255,255,255,0.28)" stroke-width="2" stroke-linecap="round" fill="none"/>
                    </svg>

                    <!-- Shield -->
                    <svg class="deco-shield" viewBox="0 0 80 96" fill="none">
                        <path d="M40 8L12 24V48C12 68 24 84 40 90C56 84 68 68 68 48V24L40 8Z" fill="rgba(255,255,255,0.1)" stroke="rgba(255,255,255,0.25)" stroke-width="2"/>
                        <path d="M32 48L38 54L50 42" stroke="rgba(255,255,255,0.4)" stroke-width="3" stroke-linecap="round" stroke-linejoin="round"/>
                    </svg>

                    <!-- Checkmark bubble -->
                    <svg class="deco-check" viewBox="0 0 56 56" fill="none">
                        <circle cx="28" cy="28" r="24" fill="rgba(255,255,255,0.18)"/>
                        <path d="M18 28L25 35L38 22" stroke="white" stroke-width="3" stroke-linecap="round" stroke-linejoin="round" opacity="0.6"/>
                    </svg>

                    <!-- Person silhouette -->
                    <svg class="deco-person" viewBox="0 0 100 160" fill="none">
                        <circle cx="50" cy="30" r="18" fill="rgba(255,255,255,0.2)"/>
                        <path d="M20 140C20 108 32 82 50 82C68 82 80 108 80 140" fill="rgba(255,255,255,0.15)"/>
                        <!-- Arm reaching toward fingerprint -->
                        <path d="M68 100C78 90 90 85 100 82" stroke="rgba(255,255,255,0.2)" stroke-width="4" stroke-linecap="round" fill="none"/>
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
    <#elseif section = "info">
    <#elseif section = "socialProviders">
    </#if>
</@layout.registrationLayout>
