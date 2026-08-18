// Minimal declarations for libchdr (CGo). Link with -lchdr.
// Install: apt install libchdr-dev (Debian/Ubuntu) or libchdr-git (Arch User Repository)
// Or build from: https://github.com/rtissera/libchdr
#ifndef CHD_LIBCHDR_H
#define CHD_LIBCHDR_H

#ifdef __cplusplus
extern "C" {
#endif

#define CHD_OPEN_READ 1

struct chd_file;

int chd_open(const char *filename, int mode, struct chd_file *parent, struct chd_file **chd_out);
int chd_read(struct chd_file *chd, unsigned int hunknum, void *buffer);
void chd_close(struct chd_file *chd);

#ifdef __cplusplus
}
#endif

#endif
