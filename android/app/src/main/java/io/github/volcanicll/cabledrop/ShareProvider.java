package io.github.volcanicll.cabledrop;

import android.content.ContentProvider;
import android.content.ContentValues;
import android.database.Cursor;
import android.database.MatrixCursor;
import android.net.Uri;
import android.os.ParcelFileDescriptor;
import android.provider.OpenableColumns;

import java.io.File;
import java.io.FileNotFoundException;
import java.io.IOException;

/**
 * Serves the files MainActivity stages for sharing, so ACTION_SEND can carry
 * a content:// URI instead of a file:// one.
 *
 * A file:// URI in EXTRA_STREAM throws FileUriExposedException on any
 * targetSdk >= 24, and the one drop-in replacement, AndroidX's FileProvider,
 * is a library this project deliberately has none of. This is the same idea
 * cut down to what an image share actually needs: read-only access to files
 * this app itself placed under cacheDir/share, nothing writable, nothing
 * listable.
 *
 * exported stays false — receiving apps get one-shot read permission through
 * the FLAG_GRANT_READ_URI_PERMISSION on the share intent, and nobody else can
 * even resolve the authority.
 */
public class ShareProvider extends ContentProvider {

    public static final String AUTHORITY = "io.github.volcanicll.cabledrop.share";

    @Override
    public boolean onCreate() {
        return true;
    }

    /** Receivers call this to learn the file's name and size for their own
     *  UI (WeChat shows it as the image's caption); a two-column cursor is
     *  the minimum that keeps those from erroring. */
    @Override
    public Cursor query(Uri uri, String[] projection, String selection,
                        String[] selectionArgs, String sortOrder) {
        File f = fileFor(uri);
        if (projection == null) {
            projection = new String[] { OpenableColumns.DISPLAY_NAME, OpenableColumns.SIZE };
        }
        MatrixCursor cursor = new MatrixCursor(projection);
        if (f != null && f.isFile()) {
            MatrixCursor.RowBuilder row = cursor.newRow();
            for (String col : projection) {
                if (OpenableColumns.DISPLAY_NAME.equals(col)) {
                    row.add(f.getName());
                } else if (OpenableColumns.SIZE.equals(col)) {
                    row.add(f.length());
                } else {
                    row.add(null);
                }
            }
        }
        return cursor;
    }

    @Override
    public String getType(Uri uri) {
        File f = fileFor(uri);
        String name = (f == null) ? uri.getLastPathSegment() : f.getName();
        return (name != null && name.endsWith(".png")) ? "image/png" : "application/octet-stream";
    }

    @Override
    public ParcelFileDescriptor openFile(Uri uri, String mode) throws FileNotFoundException {
        // The grant the share intent carries is read-only; a write mode here
        // would be a bug, not a feature.
        if (!"r".equals(mode)) {
            throw new FileNotFoundException("read-only: " + uri);
        }
        File f = fileFor(uri);
        if (f == null || !f.isFile()) {
            throw new FileNotFoundException(uri.toString());
        }
        try {
            return ParcelFileDescriptor.open(f, ParcelFileDescriptor.MODE_READ_ONLY);
        } catch (IOException e) {
            throw new FileNotFoundException(e.getMessage());
        }
    }

    /** Resolves the last path segment to a file inside cacheDir/share,
     *  refusing anything that could name a file elsewhere — segments are
     *  ours, but the guard is what makes "read-only, our directory only"
     *  true structurally rather than by convention. */
    private File fileFor(Uri uri) {
        String name = uri.getLastPathSegment();
        if (name == null || name.length() == 0
                || name.contains("/") || name.contains("\\") || name.contains("..")) {
            return null;
        }
        return new File(new File(getContext().getCacheDir(), "share"), name);
    }

    @Override
    public Uri insert(Uri uri, ContentValues values) {
        throw new UnsupportedOperationException("read-only");
    }

    @Override
    public int delete(Uri uri, String selection, String[] selectionArgs) {
        throw new UnsupportedOperationException("read-only");
    }

    @Override
    public int update(Uri uri, ContentValues values, String selection, String[] selectionArgs) {
        throw new UnsupportedOperationException("read-only");
    }
}
