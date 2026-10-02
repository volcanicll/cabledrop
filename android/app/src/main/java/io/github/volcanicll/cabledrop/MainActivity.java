package io.github.volcanicll.cabledrop;

import android.app.Activity;
import android.app.DownloadManager;
import android.content.Context;
import android.content.Intent;
import android.content.SharedPreferences;
import android.content.res.Configuration;
import android.net.Uri;
import android.os.Bundle;
import android.os.Environment;
import android.view.Gravity;
import android.view.View;
import android.view.ViewGroup;
import android.webkit.ValueCallback;
import android.webkit.JavascriptInterface;
import android.webkit.WebChromeClient;
import android.webkit.WebResourceError;
import android.webkit.WebResourceRequest;
import android.webkit.WebSettings;
import android.webkit.WebView;
import android.webkit.WebViewClient;
import android.widget.Button;
import android.widget.EditText;
import android.widget.FrameLayout;
import android.widget.LinearLayout;
import android.widget.TextView;
import android.widget.Toast;

import java.io.BufferedInputStream;
import java.io.File;
import java.io.FileOutputStream;
import java.io.IOException;
import java.io.InputStream;
import java.net.HttpURLConnection;
import java.net.URL;
import java.text.SimpleDateFormat;
import java.util.Date;
import java.util.Locale;

/**
 * A thin shell around the same page the phone's browser gets at
 * http://127.0.0.1:<port>, tunnelled through the USB cable by `adb reverse`.
 *
 * Everything on that page already works in a plain WebView — the native work
 * here is only what a browser page cannot do by itself: hand real file paths
 * to the upload input, route downloads into the system DownloadManager, and
 * explain what is wrong when the cable is unplugged.
 */
public class MainActivity extends Activity {

    private static final String PREFS = "cabledrop";
    private static final String PREF_PORT = "port";
    private static final int DEFAULT_PORT = 8765;
    private static final int FILE_CHOOSER = 1001;

    private WebView web;
    private FrameLayout root;
    private LinearLayout status;
    private TextView statusTitle;
    private TextView statusBody;
    private EditText portBox;
    private SharedPreferences prefs;
    private ValueCallback<Uri[]> filePick;

    @Override
    protected void onCreate(Bundle savedInstanceState) {
        super.onCreate(savedInstanceState);
        prefs = getSharedPreferences(PREFS, MODE_PRIVATE);

        // fitsSystemWindows keeps both the page and the status screen below
        // the status bar: targetSdk 35+ draws edge-to-edge by default.
        root = new FrameLayout(this);
        root.setFitsSystemWindows(true);

        web = new WebView(this);
        root.addView(web, new FrameLayout.LayoutParams(
                ViewGroup.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.MATCH_PARENT));

        status = buildStatusView();
        status.setVisibility(View.GONE);
        root.addView(status, new FrameLayout.LayoutParams(
                ViewGroup.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.MATCH_PARENT));

        setContentView(root, new ViewGroup.LayoutParams(
                ViewGroup.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.MATCH_PARENT));

        WebSettings s = web.getSettings();
        s.setJavaScriptEnabled(true);
        s.setDomStorageEnabled(true);
        // File and content access must be on for uploads to work. Since
        // targetSdk 30, setAllowFileAccess() defaults to false, and the file
        // picker hands back URIs — file:// from some pickers, content:// from
        // DocumentsUI — that the WebView itself has to read when it builds the
        // upload's FormData. With either switch off the page learns the file's
        // name but not its contents, and the upload dies before a byte moves.
        // The page is our own loopback content, so nothing here widens the
        // attack surface.
        s.setAllowFileAccess(true);
        s.setAllowContentAccess(true);
        // Debug builds get a DevTools socket so the page can be inspected on a
        // real phone (`adb forward` the webview_devtools_remote socket, then
        // chrome://inspect). Release builds do not: an open debugging port on
        // a loopback page is still an open port.
        boolean debuggable =
                (getApplicationInfo().flags & android.content.pm.ApplicationInfo.FLAG_DEBUGGABLE) != 0;
        WebView.setWebContentsDebuggingEnabled(debuggable);
        // Match the page canvas so the frame between cold start and first
        // paint (and any gap under short content) is not a white flash.
        web.setBackgroundColor(resolveWindowBg());

        // The page's share button asks the shell to open the system share
        // sheet: Web Share with files is Chrome 76+, and the WebView this
        // phone ships is pinned at 75 (see NativeBridge). The interface is
        // safe to expose because the WebView only ever loads our own loopback
        // page — shouldOverrideUrlLoading hands anything else to the browser.
        web.addJavascriptInterface(new NativeBridge(), "CableDropNative");

        web.setWebViewClient(new WebViewClient() {
            @Override
            public boolean shouldOverrideUrlLoading(WebView view, WebResourceRequest request) {
                Uri url = request.getUrl();
                String host = url.getHost();
                boolean local = "127.0.0.1".equals(host) || "localhost".equals(host);
                if (local) {
                    return false; // ours: load in the WebView
                }
                // A link off the loopback has nothing to do with the bridge;
                // hand it to whatever the phone normally opens links with.
                try {
                    startActivity(new Intent(Intent.ACTION_VIEW, url));
                } catch (Exception ignored) {
                }
                return true;
            }

            @Override
            public void onReceivedError(WebView view, WebResourceRequest request,
                                        WebResourceError error) {
                if (request.isForMainFrame()) {
                    showStatus();
                }
            }
        });

        web.setWebChromeClient(new WebChromeClient() {
            // The upload input on the page is a plain <input type=file>; this
            // is what turns a tap on it into a real Android file picker.
            //
            // The callback must be settled no matter what. A picker that never
            // opens (no handler for the intent) leaves filePick set with
            // nothing coming back through onActivityResult, and the page's
            // input stays locked waiting for a result that never arrives.
            @Override
            public boolean onShowFileChooser(WebView view, ValueCallback<Uri[]> callback,
                                             FileChooserParams params) {
                if (filePick != null) {
                    filePick.onReceiveValue(null);
                }
                filePick = callback;
                try {
                    startActivityForResult(params.createIntent(), FILE_CHOOSER);
                    return true;
                } catch (android.content.ActivityNotFoundException e) {
                    // Some ROMs ship no default handler for the bare
                    // createIntent() form; a chooser over the same intent
                    // still finds one. This must not recurse: try the chooser
                    // once, then give up for good.
                    try {
                        startActivityForResult(
                                Intent.createChooser(params.createIntent(), "选择文件"),
                                FILE_CHOOSER);
                        return true;
                    } catch (Exception e2) {
                        filePick.onReceiveValue(null);
                        filePick = null;
                        return false;
                    }
                } catch (Exception e) {
                    filePick.onReceiveValue(null);
                    filePick = null;
                    return false;
                }
            }
        });

        web.setDownloadListener((url, userAgent, disposition, mime, length) -> {
            try {
                String name = filenameFrom(url, disposition);
                DownloadManager.Request req = new DownloadManager.Request(Uri.parse(url));
                req.setTitle(name);
                req.setNotificationVisibility(DownloadManager.Request.VISIBILITY_VISIBLE_NOTIFY_COMPLETED);
                req.setDestinationInExternalPublicDir(Environment.DIRECTORY_DOWNLOADS, name);
                DownloadManager dm = (DownloadManager) getSystemService(Context.DOWNLOAD_SERVICE);
                dm.enqueue(req);
                Toast.makeText(this, "开始下载 " + name, Toast.LENGTH_SHORT).show();
            } catch (Exception e) {
                Toast.makeText(this, "下载失败: " + e.getMessage(), Toast.LENGTH_SHORT).show();
            }
        });

        if (savedInstanceState != null) {
            web.restoreState(savedInstanceState);
        } else {
            loadPage();
        }
    }

    private void loadPage() {
        web.loadUrl("http://127.0.0.1:" + prefs.getInt(PREF_PORT, DEFAULT_PORT) + "/");
    }

    /** Exposed to the page as window.CableDropNative: the things a web page
     *  cannot do by itself but the APK shell can. Right now that is one thing
     *  — opening the system share sheet for the clipboard image. A browser
     *  would use Web Share for that, but sharing a *file* is Web Share Level
     *  2 (Chrome 76+), and the WebView this project's reference phone is
     *  pinned at is Chrome 75, so inside the APK the page has no API left and
     *  this bridge is the only road. */
    private class NativeBridge {

        /** Called from the page's share button, on the JS bridge thread.
         *  The image never crosses JNI as a base64 string: native fetches it
         *  itself from the same loopback server the page reads, stages it in
         *  the cache, then opens the chooser on the main thread. */
        @JavascriptInterface
        public void shareClipImage() {
            new Thread(() -> {
                String name = null;
                Exception fail = null;
                try {
                    name = stageClipImage();
                } catch (Exception e) {
                    fail = e;
                }
                onStaged(name, fail);
            }, "cabledrop-share").start();
        }

        /** The landing after the fetch: an error toast or the share sheet,
         *  either way back on the main thread. */
        private void onStaged(final String name, final Exception fail) {
            runOnUiThread(() -> {
                if (isFinishing() || isDestroyed()) return;
                if (fail != null) {
                    Toast.makeText(MainActivity.this, fail.getMessage(), Toast.LENGTH_LONG).show();
                    return;
                }
                shareStaged(name);
            });
        }

        /** Fetches /api/clip/image and writes the PNG to cacheDir/share,
         *  returning its file name. Every failure throws with a message
         *  written for a toast, so a dead end is always named: a silent
         *  nothing after a tap reads as "the bridge is broken". */
        private String stageClipImage() throws Exception {
            int port = prefs.getInt(PREF_PORT, DEFAULT_PORT);
            HttpURLConnection conn = (HttpURLConnection)
                    new URL("http://127.0.0.1:" + port + "/api/clip/image").openConnection();
            conn.setConnectTimeout(4000);
            conn.setReadTimeout(8000);
            int code = conn.getResponseCode();
            if (code == 404) {
                throw new IOException("电脑剪贴板里现在没有图片");
            }
            if (code != 200) {
                throw new IOException("取图失败（HTTP " + code + "），请重试");
            }
            String name = "cabledrop-" +
                    new SimpleDateFormat("yyyyMMdd-HHmmss", Locale.US).format(new Date()) + ".png";
            File dir = new File(getCacheDir(), "share");
            if (!dir.isDirectory() && !dir.mkdirs()) {
                throw new IOException("无法写入应用缓存，分享中止");
            }
            // A staged file must outlive the share sheet — the receiving app
            // reads it whenever the user finishes tapping — but a day-old one
            // is surely done sharing and is only costing cache space.
            long stale = System.currentTimeMillis() - 24 * 60 * 60 * 1000L;
            File[] old = dir.listFiles();
            if (old != null) {
                for (File f : old) {
                    if (f.lastModified() < stale) f.delete();
                }
            }
            File out = new File(dir, name);
            InputStream in = new BufferedInputStream(conn.getInputStream());
            FileOutputStream fout = new FileOutputStream(out);
            try {
                byte[] buf = new byte[16 * 1024];
                for (int n; (n = in.read(buf)) != -1; ) {
                    fout.write(buf, 0, n);
                }
            } finally {
                try { fout.close(); } catch (IOException ignored) { }
                try { in.close(); } catch (IOException ignored) { }
            }
            return name;
        }

        /** Opens the system share sheet over the staged file. Which apps
         *  appear — WeChat, QQ, whatever else — is the sheet's own business:
         *  anything declaring a SEND handler for images lands there. */
        private void shareStaged(String name) {
            Uri uri = new Uri.Builder()
                    .scheme("content")
                    .authority(ShareProvider.AUTHORITY)
                    .appendPath(name)
                    .build();
            Intent send = new Intent(Intent.ACTION_SEND);
            send.setType("image/png");
            send.putExtra(Intent.EXTRA_STREAM, uri);
            // The flag on the inner intent is what grants the receiver read
            // access. The chooser gets it too: on some OS levels it forwards
            // the stream without the grant unless its own intent carries it.
            send.addFlags(Intent.FLAG_GRANT_READ_URI_PERMISSION);
            Intent chooser = Intent.createChooser(send, "分享图片");
            chooser.addFlags(Intent.FLAG_GRANT_READ_URI_PERMISSION);
            try {
                startActivity(chooser);
            } catch (Exception e) {
                Toast.makeText(MainActivity.this, "没有可用的分享方式", Toast.LENGTH_LONG).show();
            }
        }
    }

    /** Shown when the page cannot be reached: the cable is out or the desktop
     *  app is not serving. Carries the port so the rare fallback port (8765
     *  already taken on the computer) can be matched by hand. */
    private LinearLayout buildStatusView() {
        LinearLayout box = new LinearLayout(this);
        box.setOrientation(LinearLayout.VERTICAL);
        box.setGravity(Gravity.CENTER);
        box.setPadding(dp(32), dp(32), dp(32), dp(32));
        boolean night = isNightMode();
        box.setBackgroundColor(night ? 0xFF131315 : 0xFFF5F5F7);
        int titleColor = night ? 0xFFF2F2F5 : 0xFF1D1D1F;
        int bodyColor = night ? 0xFFA2A2AB : 0xFF6E6E73;

        statusTitle = new TextView(this);
        statusTitle.setText("CableDrop 未连接");
        statusTitle.setTextSize(20);
        statusTitle.setTextColor(titleColor);
        statusTitle.setGravity(Gravity.CENTER);

        statusBody = new TextView(this);
        statusBody.setText("用 USB 线连接手机和电脑，电脑端打开 CableDrop\n并保持手机网页开启，然后点下面的重试。");
        statusBody.setTextSize(14);
        statusBody.setLineSpacing(dp(3), 1f);
        statusBody.setTextColor(bodyColor);
        statusBody.setGravity(Gravity.CENTER);
        statusBody.setPadding(0, dp(10), 0, 0);

        LinearLayout row = new LinearLayout(this);
        row.setOrientation(LinearLayout.HORIZONTAL);
        row.setGravity(Gravity.CENTER);

        portBox = new EditText(this);
        portBox.setInputType(android.text.InputType.TYPE_CLASS_NUMBER);
        portBox.setHint("端口");
        portBox.setText(String.valueOf(prefs.getInt(PREF_PORT, DEFAULT_PORT)));
        LinearLayout.LayoutParams portLp = new LinearLayout.LayoutParams(dp(110), ViewGroup.LayoutParams.WRAP_CONTENT);
        row.addView(portBox, portLp);

        Button retry = new Button(this);
        retry.setText("重试");
        retry.setOnClickListener(v -> {
            int port = DEFAULT_PORT;
            try {
                port = Integer.parseInt(portBox.getText().toString().trim());
            } catch (NumberFormatException ignored) {
            }
            prefs.edit().putInt(PREF_PORT, port).apply();
            status.setVisibility(View.GONE);
            loadPage();
        });
        LinearLayout.LayoutParams retryLp = new LinearLayout.LayoutParams(
                ViewGroup.LayoutParams.WRAP_CONTENT, ViewGroup.LayoutParams.WRAP_CONTENT);
        retryLp.setMargins(dp(10), 0, 0, 0);
        row.addView(retry, retryLp);

        box.addView(statusTitle);
        box.addView(statusBody);
        LinearLayout.LayoutParams rowLp = new LinearLayout.LayoutParams(
                ViewGroup.LayoutParams.WRAP_CONTENT, ViewGroup.LayoutParams.WRAP_CONTENT);
        rowLp.topMargin = dp(20);
        box.addView(row, rowLp);
        return box;
    }

    /** Whether the system is in dark mode, so the native screens can match
     *  the page's own dark palette instead of flashing white. */
    private boolean isNightMode() {
        int mask = getResources().getConfiguration().uiMode
                & Configuration.UI_MODE_NIGHT_MASK;
        return mask == Configuration.UI_MODE_NIGHT_YES;
    }

    /** Whether the page can honour prefers-color-scheme.
     *
     *  The WebView an app gets is the ROM's, and ROMs pin them for years: the
     *  phone this was developed against serves its pages from an AOSP WebView
     *  stuck at Chrome 75, where the media query does not exist and the page
     *  renders light whatever the system says. Framing a light page in a dark
     *  window looks worse than not following dark mode at all, so the frame
     *  follows the page rather than the system in that case.
     *
     *  prefers-color-scheme landed in Chrome 76. */
    private boolean pageSupportsDark() {
        if (android.os.Build.VERSION.SDK_INT < android.os.Build.VERSION_CODES.O) {
            return false;
        }
        android.content.pm.PackageInfo pkg = WebView.getCurrentWebViewPackage();
        if (pkg == null || pkg.versionName == null) {
            return false;
        }
        String major = pkg.versionName.split("\\.")[0];
        try {
            return Integer.parseInt(major) >= 76;
        } catch (NumberFormatException e) {
            return false;
        }
    }

    private int resolveWindowBg() {
        return (isNightMode() && pageSupportsDark()) ? 0xFF131315 : 0xFFF5F5F7;
    }

    /** The activity handles uiMode itself (see the manifest), so it is not
     *  recreated when the user flips dark mode: re-apply the native colours
     *  here so the frame and the offline screen keep matching the page. */
    @Override
    public void onConfigurationChanged(Configuration newConfig) {
        super.onConfigurationChanged(newConfig);
        web.setBackgroundColor(resolveWindowBg());
        rebuildStatusColors();
    }

    private void rebuildStatusColors() {
        if (status == null || statusTitle == null) return;
        boolean night = isNightMode();
        status.setBackgroundColor(night ? 0xFF131315 : 0xFFF5F5F7);
        statusTitle.setTextColor(night ? 0xFFF2F2F5 : 0xFF1D1D1F);
        statusBody.setTextColor(night ? 0xFFA2A2AB : 0xFF6E6E73);
    }

    private void showStatus() {
        status.setVisibility(View.VISIBLE);
    }

    /** The server sends `filename*=UTF-8''...` so Chinese names survive; the
     *  plain filename= form and the URL's last segment are the fallbacks. */
    private static String filenameFrom(String url, String disposition) {
        if (disposition != null) {
            java.util.regex.Matcher m = java.util.regex.Pattern
                    .compile("filename\\*=UTF-8''([^;]+)", java.util.regex.Pattern.CASE_INSENSITIVE)
                    .matcher(disposition);
            if (m.find()) {
                return Uri.decode(m.group(1));
            }
            m = java.util.regex.Pattern
                    .compile("filename=\"?([^\";]+)\"?", java.util.regex.Pattern.CASE_INSENSITIVE)
                    .matcher(disposition);
            if (m.find()) {
                return Uri.decode(m.group(1).trim());
            }
        }
        String seg = Uri.parse(url).getLastPathSegment();
        return (seg == null || seg.isEmpty()) ? "download" : Uri.decode(seg);
    }

    @Override
    protected void onActivityResult(int requestCode, int resultCode, Intent data) {
        if (requestCode == FILE_CHOOSER) {
            if (filePick != null) {
                filePick.onReceiveValue(WebChromeClient.FileChooserParams.parseResult(resultCode, data));
                filePick = null;
            }
            return;
        }
        super.onActivityResult(requestCode, resultCode, data);
    }

    @Override
    public void onBackPressed() {
        if (web.canGoBack()) {
            web.goBack();
        } else {
            super.onBackPressed();
        }
    }

    @Override
    protected void onSaveInstanceState(Bundle outState) {
        super.onSaveInstanceState(outState);
        web.saveState(outState);
    }

    private int dp(int v) {
        return Math.round(v * getResources().getDisplayMetrics().density);
    }
}
