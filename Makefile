PREFIX = $(PWD)/build

# reference files in each subproject to ensure git fully checks the project out
SNAPPY           = third_party/snappy/CMakeLists.txt
ZSTD             = third_party/zstd/Makefile
SPARKEY          = third_party/sparkey/configure.ac

ZSTD_LIB         = $(PREFIX)/lib/libzstd.a
SNAPPY_LIB       = $(PREFIX)/lib/libsnappy.a
SPARKEY_LIB      = $(PREFIX)/lib/libsparkey.a
LIBS             = $(ZSTD_LIB) $(SNAPPY_LIB) $(SPARKEY_LIB)

ALL_SUBMODULES   = $(SPARKEY) $(SNAPPY) $(ZSTD)

CGO_ENV          = CGO_LDFLAGS="-L$(PREFIX)/lib -l$(CXX_LIB) $(SPARKEY_LIB) $(ZSTD_LIB) $(SNAPPY_LIB)" CGO_CFLAGS="-I$(PREFIX)/include"

CONFIG           = Makefile go.mod go.sum

# by default macOS uses libc++ and linux distros use libstdc++
ifeq ($(UNAME_S),Darwin)
CXX_LIB          = c++
else
CXX_LIB          = stdc++
endif

# quiet output, but allow us to look at what commands are being
# executed by passing 'V=1' to make, without requiring temporarily
# editing the Makefile.
ifneq ($V, 1)
MAKEFLAGS       += -s
endif

.SUFFIXES:
.SUFFIXES: .cc .c .go .o .d .test

all: test

$(ALL_SUBMODULES):
	@echo "  GIT   $@"
	git submodule update --init --recursive
	touch -c $@

$(ZSTD_LIB): $(ALL_SUBMODULES)
	@echo "  BUILD $@"
	cd third_party/zstd && make clean && make install -j8 PREFIX=$(PREFIX) && make clean

$(SNAPPY_LIB): $(ALL_SUBMODULES)
	@echo "  BUILD $@"
	cd third_party/snappy && rm -rf build && mkdir build && cd build && cmake -DCMAKE_INSTALL_PREFIX=$(PREFIX) .. && cmake --build . && cmake --install . && cd .. && rm -r build

$(SPARKEY_LIB): $(ALL_SUBMODULES)
	@echo "  BUILD $@"
	rm -rf $(PREFIX)/lib/*.dylib $(PREFIX)/lib/*.so && cd third_party/sparkey && autoreconf -fvi && CPPFLAGS="-I$(PREFIX)/include" LDFLAGS="-L$(PREFIX)/lib -l$(CXX_LIB)" ./configure --prefix=$(PREFIX) --enable-static --disable-shared && make -j8 && make install && make clean && rm -f configure~

lib: $(LIBS)
	@echo "  BUILD $@"
	$(CGO_ENV) go build

test: lib
	@echo "  TEST  $@"
	$(CGO_ENV) go test -bench=.Get -cpu 1,2,4,8
	$(CGO_ENV) go test -bench=.Create



clean:
	rm -rf build


distclean: clean

.PHONY: all clean distclean lib
